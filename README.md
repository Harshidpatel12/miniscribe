# miniscribe

A CPU-friendly local speech-to-text CLI wrapping `sherpa-onnx` for automatic speech recognition, Silero VAD chunking, and pyannote speaker diarization.

Inspired by [Soniqo](https://soniqo.audio/cli), it delivers offline speech-to-text with no external servers, API keys, or heavy framework dependencies.

## Prerequisites

- **Go**: 1.22 or higher
- **FFmpeg**: Required for decoding non-WAV/WAV audio file formats to the expected PCM stream. Verify via `ffmpeg -version`.
- **CGO**: A C compiler (like `gcc` or `clang`) must be installed on your system.

## Installation

### Single-Command Installer (Linux & macOS)

To install `miniscribe` and its required prebuilt shared libraries, run using **curl**:

```bash
curl -fsSL https://raw.githubusercontent.com/Harshidpatel12/miniscribe/main/install.sh | bash
```

Or using **wget**:

```bash
wget -qO- https://raw.githubusercontent.com/Harshidpatel12/miniscribe/main/install.sh | bash
```

*Note: This script automatically detects your Operating System and Architecture, downloads the pre-built tarball from GitHub Releases, extracts it, and links the binary to your executable path (`/usr/local/bin` or `~/.local/bin` if running without root).*

## Build

Compile the single binary using CGO:

```bash
cd miniscribe
make build
```

This compiles the executable into `./bin/miniscribe`.

---

## 5 Copy-Paste Examples

### 1. Download Model Assets
Pull any of the curated model architectures to your local cache:
```bash
./bin/miniscribe models pull moonshine
./bin/miniscribe models pull whisper
./bin/miniscribe models pull diarization
```

### 2. Basic Transcription
Transcribe any audio file (WAV, MP3, M4A, FLAC, etc.):
```bash
./bin/miniscribe transcribe conversation.mp3
```

### 3. CPU Thread Management
Limit or tune CPU core utilization for server friendliness:
```bash
./bin/miniscribe transcribe lecture.wav --model whisper --threads 4
```

### 4. Speaker Diarization (Who Spoke When)
Diarize speaker turns, timestamp their entries, and transcribe segments:
```bash
./bin/miniscribe transcribe meeting.wav --diarize
```
*If you know the speaker count in advance, force auto-clustering:*
```bash
./bin/miniscribe transcribe meeting.wav --diarize --num-speakers 3
```

### 5. Pipe-Friendly JSON Outputs
Output structured data for post-processing pipelines:
```bash
./bin/miniscribe transcribe interview.m4a --format json | jq .
```

---

## CLI Catalog & Options

```
miniscribe transcribe <audio-file> [flags]

Flags:
  --model string        Model alias (moonshine, whisper, parakeet) (default "moonshine")
  --threads int         Number of CPU threads to use (default CPU_CORES - 2)
  --format string       Output format (text, json) (default "text")
  --diarize             Enable pyannote-based speaker diarization
  --num-speakers int    Pre-defined cluster count for diarization (default -1)
  --model-dir string    Custom models folder path (overrides SPEECH_MODEL_DIR env)
  --chunk-size float    VAD auto-chunking duration limit in seconds (default 30.0)
  --overlap float       Overlap window between VAD chunks in seconds (default 2.0)
```

To list local and available models:
```bash
./bin/miniscribe models list
```

## Model Cache Directory
By default, models are downloaded to `~/.cache/speech/models`. You can override this using the `SPEECH_MODEL_DIR` environment variable, or passing `--model-dir` at runtime.
