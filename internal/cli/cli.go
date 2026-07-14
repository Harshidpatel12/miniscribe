package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Harshidpatel12/miniscribe/internal/asr"
	"github.com/Harshidpatel12/miniscribe/internal/audio"
	"github.com/Harshidpatel12/miniscribe/internal/diarize"
	"github.com/Harshidpatel12/miniscribe/internal/models"
	"github.com/Harshidpatel12/miniscribe/internal/vad"
)

var (
	modelName   string
	threadsStr  string
	format      string
	diarizeFlag bool
	numSpeakers int
	modelDir    string
	chunkSize   float32
	overlap     float32
	verbose     bool
)

func logStatus(format string, a ...interface{}) {
	if !verbose {
		return
	}
	// Print in faint gray to stderr to separate status info from actual stdout output.
	fmt.Fprintf(os.Stderr, "\033[2m"+format+"\033[0m", a...)
}

// RootCmd is the base command for the CLI.
var RootCmd = &cobra.Command{
	Use:   "miniscribe",
	Short: "miniscribe: CPU-friendly local speech-to-text CLI",
	Long:  "miniscribe is a CPU-friendly local speech-to-text CLI wrapping sherpa-onnx.",
}

// TranscribeCmd performs ASR or Diarization on an audio file.
var TranscribeCmd = &cobra.Command{
	Use:   "transcribe <audio-file>",
	Short: "Transcribe an audio file offline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		audioPath := args[0]

		// Resolve model cache directory
		cacheDir, err := models.GetCacheDir(modelDir)
		if err != nil {
			return fmt.Errorf("failed to resolve model directory: %w", err)
		}

		// Parse threads configuration ('all' or an integer)
		var threads int
		if threadsStr == "all" {
			threads = runtime.NumCPU()
		} else {
			val, err := strconv.Atoi(threadsStr)
			if err != nil {
				return fmt.Errorf("invalid value for --threads: must be an integer or 'all'")
			}
			threads = val
		}
		if threads < 1 {
			threads = 1
		}

		// Redirect model download progress depending on verbosity
		var progress io.Writer = io.Discard
		if verbose {
			progress = os.Stderr
		}

		// Auto-pull models synchronously to ensure disk assets exist before concurrent initialization.
		installed, err := models.IsInstalled(cacheDir, modelName)
		if err != nil {
			return err
		}
		if !installed {
			logStatus("Model %q not found locally. Downloading...\n", modelName)
			if err := models.Pull(cacheDir, modelName, progress); err != nil {
				return fmt.Errorf("failed to download model %q: %w", modelName, err)
			}
		}

		vadModelPath := filepath.Join(cacheDir, "silero_vad.onnx")
		vadInstalled, err := models.IsInstalled(cacheDir, "silero-vad")
		if err != nil {
			return err
		}
		if !vadInstalled {
			logStatus("Silero VAD not found locally. Downloading...\n")
			if err := models.Pull(cacheDir, "silero-vad", progress); err != nil {
				return fmt.Errorf("failed to download Silero VAD: %w", err)
			}
		}

		if diarizeFlag {
			diarizeInstalled, err := models.IsInstalled(cacheDir, "diarization")
			if err != nil {
				return err
			}
			if !diarizeInstalled {
				logStatus("Diarization models not found locally. Downloading...\n")
				if err := models.Pull(cacheDir, "diarization", progress); err != nil {
					return fmt.Errorf("failed to download diarization models: %w", err)
				}
			}
		}

		// Spawn Goroutine 1: Load ASR engine in parallel
		type ASRResult struct {
			Engine *asr.Recognizer
			Err    error
		}
		asrChan := make(chan ASRResult, 1)
		go func() {
			logStatus("Loading model %s (%d threads)...\n", modelName, threads)
			engine, err := asr.NewRecognizer(cacheDir, modelName, threads)
			asrChan <- ASRResult{Engine: engine, Err: err}
		}()

		// Decode audio to PCM on main thread (takes ~100-300ms)
		logStatus("Decoding audio file %s...\n", audioPath)
		samples, err := audio.DecodeToPCM(audioPath)
		if err != nil {
			return fmt.Errorf("audio decoding failed: %w", err)
		}
		duration := float32(len(samples)) / 16000.0
		logStatus("Audio duration: %.2fs (%d samples)\n", duration, len(samples))

		// Process speaker diarization or VAD chunking sequentially on the main thread.
		// Since ASR loading is still running in the background, this will overlap with it
		// without launching multiple concurrent heavy ONNX loader goroutines at the exact same instant.
		var turns []diarize.SegmentResult
		var chunks []vad.Chunk

		if diarizeFlag {
			logStatus("Running speaker diarization...\n")
			diarizer, err := diarize.NewDiarizer(cacheDir, threads, numSpeakers)
			if err != nil {
				return fmt.Errorf("failed to load diarization engine: %w", err)
			}
			segmentTurns := diarizer.Process(samples)
			diarizer.Close()

			// Wait for ASR engine to finish loading (we need it to transcribe the speaker turns)
			asrRes := <-asrChan
			if asrRes.Err != nil {
				return asrRes.Err
			}
			engine := asrRes.Engine
			defer engine.Close()

			// Run transcription on each speaker turn using the loaded ASR engine
			turns, err = diarize.TranscribeSegments(samples, segmentTurns, engine)
			if err != nil {
				return err
			}

			if format == "json" {
				texts := make([]string, len(turns))
				for i, turn := range turns {
					texts[i] = turn.Text
				}
				out := struct {
					Segments interface{} `json:"segments"`
					Text     string      `json:"text"`
				}{
					Segments: turns,
					Text:     strings.Join(texts, " "),
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			// Format text output per speaker turn
			for _, turn := range turns {
				fmt.Printf("[%s] %.2f-%.2f: %s\n", turn.Speaker, turn.Start, turn.End, turn.Text)
			}
			return nil
		}

		if duration > chunkSize {
			logStatus("Audio duration exceeds chunk size (%.1fs > %.1fs). Using VAD auto-chunking.\n", duration, chunkSize)
			segments, err := vad.DetectSpeechSegments(vadModelPath, samples, 1)
			if err != nil {
				return fmt.Errorf("VAD segmentation failed: %w", err)
			}
			chunks = vad.GroupSegments(segments, len(samples), chunkSize, overlap)
		}

		// Wait for ASR engine to finish loading
		asrRes := <-asrChan
		if asrRes.Err != nil {
			return asrRes.Err
		}
		engine := asrRes.Engine
		defer engine.Close()

		// Standard ASR Path
		var text string
		if duration > chunkSize {
			logStatus("Transcribing chunks...\n")
			results := make([]string, 0, len(chunks))
			for _, chunk := range chunks {
				chunkSamples := samples[chunk.StartSample:chunk.EndSample]
				chunkText, err := engine.Transcribe(chunkSamples)
				if err != nil {
					return fmt.Errorf("failed to transcribe chunk [%.1f-%.1fs]: %w", chunk.StartSec, chunk.EndSec, err)
				}
				if chunkText != "" {
					results = append(results, chunkText)
				}
			}
			text = asr.MergeTranscriptions(results)
		} else {
			logStatus("Transcribing...\n")
			var err error
			text, err = engine.Transcribe(samples)
			if err != nil {
				return fmt.Errorf("transcription failed: %w", err)
			}
		}

		if format == "json" {
			out := struct {
				Text string `json:"text"`
			}{Text: text}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Println(text)
		return nil
	},
}

// ModelsCmd manages speech models.
var ModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage local ASR and Diarization models",
}

var ModelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all supported and installed models",
	RunE: func(cmd *cobra.Command, args []string) error {
		cacheDir, err := models.GetCacheDir(modelDir)
		if err != nil {
			return err
		}

		fmt.Printf("Model cache directory: %s\n\n", cacheDir)
		fmt.Printf("%-20s %-12s %s\n", "MODEL ALIAS", "STATUS", "DESCRIPTION")
		fmt.Println(strings.Repeat("-", 70))

		modelKeys := []string{"moonshine", "whisper", "parakeet", "diarization", "silero-vad"}
		for _, key := range modelKeys {
			installed, err := models.IsInstalled(cacheDir, key)
			status := "Available"
			if err == nil && installed {
				status = "Installed"
			}
			if err != nil {
				status = "Error"
			}

			desc := ""
			if info, ok := models.Catalog[key]; ok {
				desc = info.Description
			} else if key == "diarization" {
				desc = "Pyannote Segmentation + 3D-Speaker Embeddings (required for --diarize)"
			}

			fmt.Printf("%-20s %-12s %s\n", key, status, desc)
		}
		return nil
	},
}

var ModelsPullCmd = &cobra.Command{
	Use:   "pull <model-alias>",
	Short: "Download a model to the local cache",
	Long:  "Download models to the local cache directory. Available: moonshine, whisper, parakeet, diarization, silero-vad.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cacheDir, err := models.GetCacheDir(modelDir)
		if err != nil {
			return err
		}
		// models.Pull already returns a clear error for unknown aliases.
		return models.Pull(cacheDir, args[0], os.Stderr)
	},
}

func init() {
	defaultThreads := runtime.NumCPU() - 2
	if defaultThreads < 1 {
		defaultThreads = 1
	}

	TranscribeCmd.Flags().StringVarP(&modelName, "model", "m", "moonshine", "Model alias to use (moonshine, whisper, parakeet)")
	TranscribeCmd.Flags().StringVarP(&threadsStr, "threads", "t", strconv.Itoa(defaultThreads), "Number of CPU threads to use ('all' or integer)")
	TranscribeCmd.Flags().StringVarP(&format, "format", "f", "text", "Output format (text, json)")
	TranscribeCmd.Flags().BoolVarP(&diarizeFlag, "diarize", "d", false, "Enable speaker diarization (turns + speakers + text)")
	TranscribeCmd.Flags().IntVarP(&numSpeakers, "num-speakers", "n", -1, "Number of speakers if known in advance (forces auto-clustering if -1)")
	TranscribeCmd.Flags().StringVarP(&modelDir, "model-dir", "D", "", "Custom path to local models cache (overrides SPEECH_MODEL_DIR)")
	TranscribeCmd.Flags().Float32VarP(&chunkSize, "chunk-size", "c", 30.0, "Audio chunk duration threshold for VAD in seconds")
	TranscribeCmd.Flags().Float32VarP(&overlap, "overlap", "o", 2.0, "VAD audio chunk overlap duration in seconds")
	TranscribeCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print verbose progress and status logs")

	ModelsCmd.PersistentFlags().StringVarP(&modelDir, "model-dir", "D", "", "Custom path to local models cache (overrides SPEECH_MODEL_DIR)")

	ModelsCmd.AddCommand(ModelsListCmd, ModelsPullCmd)

	RootCmd.AddCommand(TranscribeCmd, ModelsCmd)
}
