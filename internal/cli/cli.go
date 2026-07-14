package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"miniscribe/internal/asr"
	"miniscribe/internal/audio"
	"miniscribe/internal/diarize"
	"miniscribe/internal/models"
)

var (
	modelName   string
	threads     int
	format      string
	diarizeFlag bool
	numSpeakers int
	modelDir    string
	chunkSize   float32
	overlap     float32
)

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

		// Auto-pull ASR model on first use.
		installed, err := models.IsInstalled(cacheDir, modelName)
		if err != nil {
			return err
		}
		if !installed {
			fmt.Fprintf(os.Stderr, "Model %q not found locally. Downloading...\n", modelName)
			if err := models.Pull(cacheDir, modelName, os.Stderr); err != nil {
				return fmt.Errorf("failed to download model %q: %w", modelName, err)
			}
		}

		// Auto-pull Silero VAD on first use (required for long-form chunking).
		vadModelPath := filepath.Join(cacheDir, "silero_vad.onnx")
		vadInstalled, err := models.IsInstalled(cacheDir, "silero-vad")
		if err != nil {
			return err
		}
		if !vadInstalled {
			fmt.Fprintf(os.Stderr, "Silero VAD not found locally. Downloading...\n")
			if err := models.Pull(cacheDir, "silero-vad", os.Stderr); err != nil {
				return fmt.Errorf("failed to download Silero VAD: %w", err)
			}
		}

		// Decode audio to PCM
		fmt.Fprintf(os.Stderr, "Decoding audio file %s...\n", audioPath)
		samples, err := audio.DecodeToPCM(audioPath)
		if err != nil {
			return fmt.Errorf("audio decoding failed: %w", err)
		}
		duration := float32(len(samples)) / 16000.0
		fmt.Fprintf(os.Stderr, "Audio duration: %.2fs (%d samples)\n", duration, len(samples))

		// Initialize ASR engine
		fmt.Fprintf(os.Stderr, "Loading model %s (%d threads)...\n", modelName, threads)
		engine, err := asr.NewRecognizer(cacheDir, modelName, threads)
		if err != nil {
			return fmt.Errorf("failed to load ASR engine: %w", err)
		}
		defer engine.Close()

		if diarizeFlag {
			// Auto-pull diarization models on first use.
			diarizeInstalled, err := models.IsInstalled(cacheDir, "diarization")
			if err != nil {
				return err
			}
			if !diarizeInstalled {
				fmt.Fprintf(os.Stderr, "Diarization models not found locally. Downloading...\n")
				if err := models.Pull(cacheDir, "diarization", os.Stderr); err != nil {
					return fmt.Errorf("failed to download diarization models: %w", err)
				}
			}
			fmt.Fprintf(os.Stderr, "Running speaker diarization...\n")
			turns, err := diarize.Diarize(samples, cacheDir, engine, threads, numSpeakers)
			if err != nil {
				return fmt.Errorf("diarization failed: %w", err)
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

		// Standard ASR
		fmt.Fprintf(os.Stderr, "Transcribing...\n")
		var text string
		// If audio is longer than chunk size, use VAD chunking to prevent OOM
		if duration > chunkSize {
			fmt.Fprintf(os.Stderr, "Audio duration exceeds chunk size (%.1fs > %.1fs). Using VAD auto-chunking.\n", duration, chunkSize)
			text, err = engine.TranscribeWithChunking(samples, vadModelPath, chunkSize, overlap)
		} else {
			text, err = engine.Transcribe(samples)
		}
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
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
	TranscribeCmd.Flags().IntVarP(&threads, "threads", "t", defaultThreads, "Number of CPU threads to use for ONNX inference")
	TranscribeCmd.Flags().StringVarP(&format, "format", "f", "text", "Output format (text, json)")
	TranscribeCmd.Flags().BoolVarP(&diarizeFlag, "diarize", "d", false, "Enable speaker diarization (turns + speakers + text)")
	TranscribeCmd.Flags().IntVarP(&numSpeakers, "num-speakers", "n", -1, "Number of speakers if known in advance (forces auto-clustering if -1)")
	TranscribeCmd.Flags().StringVarP(&modelDir, "model-dir", "D", "", "Custom path to local models cache (overrides SPEECH_MODEL_DIR)")
	TranscribeCmd.Flags().Float32VarP(&chunkSize, "chunk-size", "c", 30.0, "Audio chunk duration threshold for VAD in seconds")
	TranscribeCmd.Flags().Float32VarP(&overlap, "overlap", "o", 2.0, "VAD audio chunk overlap duration in seconds")

	ModelsCmd.PersistentFlags().StringVarP(&modelDir, "model-dir", "D", "", "Custom path to local models cache (overrides SPEECH_MODEL_DIR)")

	ModelsCmd.AddCommand(ModelsListCmd, ModelsPullCmd)

	RootCmd.AddCommand(TranscribeCmd, ModelsCmd)
}
