# miniscribe

A lightning-fast, CPU-friendly local speech-to-text CLI tool. Transcribe audio files offline with automatic voice activity detection (VAD) and speaker diarization (who spoke when) in seconds—with no external servers, API keys, or internet required.

---

## 🚀 Quick Install (Linux & macOS)

Get started instantly using one of the following commands. The installer will automatically configure `miniscribe` and download the necessary dependencies for your system.

**Using curl:**
```bash
curl -fsSL https://raw.githubusercontent.com/Harshidpatel12/miniscribe/main/install.sh | bash
```

**Using wget:**
```bash
wget -qO- https://raw.githubusercontent.com/Harshidpatel12/miniscribe/main/install.sh | bash
```

---

## 📖 User Guide

### Available Commands

| Command | Description | Example |
| :--- | :--- | :--- |
| `miniscribe transcribe <file>` | Transcribe any audio file to text. Auto-splits long files. | `miniscribe transcribe meeting.wav` |
| `miniscribe models list` | Show all available and locally installed AI models. | `miniscribe models list` |
| `miniscribe models pull <alias>` | Download an AI model to your local cache. | `miniscribe models pull moonshine` |

### Supported Models

Before transcribing, download your desired model using `miniscribe models pull <model-name>`.

| Model Name | Description | Size | Best For |
| :--- | :--- | :--- | :--- |
| `moonshine` | Moonshine base English (int8 quantized) | ~54 MB | Fast, accurate, resource-friendly transcription |
| `whisper` | Whisper small English (int8 quantized) | ~480 MB | High-quality, robust transcription for noisy audio |
| `parakeet` | NeMo Parakeet TDT 0.6b English (int8 quantized) | ~160 MB | Balanced speed and accuracy |
| `diarization` | Pyannote Segmentation + 3D-Speaker Embeddings | ~100 MB | Required if you want to use the `--diarize` flag |
| `silero-vad` | Silero Voice Activity Detector | ~640 KB | Automatically downloaded (required for long-form chunking) |

### Transcription Options (Flags)

Tailor your transcription using these options:

| Flag | Default | Description | Example |
| :--- | :--- | :--- | :--- |
| `--model` | `moonshine` | Choose ASR model: `moonshine`, `whisper`, or `parakeet` | `--model whisper` |
| `--format` | `text` | Output format: `text` (plain text/turns) or `json` (metadata segments) | `--format json` |
| `--diarize` | `false` | Enable speaker identification (requires `diarization` model) | `--diarize` |
| `--num-speakers` | `-1` (auto) | Set exact number of speakers if known | `--num-speakers 3` |
| `--threads` | `CPU_CORES - 2` | Limit CPU thread count for neural network computation | `--threads 4` |
| `--model-dir` | `~/.cache/speech/models` | Custom models folder path | `--model-dir ./my-models` |
| `--chunk-size` | `30.0` | Maximum segment length in seconds before auto-splitting | `--chunk-size 20.0` |
| `--overlap` | `2.0` | Overlap window between voice chunks | `--overlap 1.5` |

---

## 💡 Quick Examples

### 1. Simple Transcription
```bash
miniscribe transcribe lecture.mp3
```

### 2. Identify Who Spoke When (Diarization)
Make sure to run `miniscribe models pull diarization` first, then run:
```bash
miniscribe transcribe meeting.wav --diarize
```

### 3. Output to JSON for Post-Processing
```bash
miniscribe transcribe podcast.m4a --format json | jq .
```

### 4. Optimize CPU Threads for Server Use
```bash
miniscribe transcribe conversation.wav --threads 4 --model whisper
```

---

## 🛠️ Developer Guide

If you want to build `miniscribe` from source or contribute to the project:

### Prerequisites
- **Go**: Version 1.22 or higher
- **FFmpeg**: Installed and in your system PATH (required for audio decoding)
- **CGO**: A C compiler (like `gcc` or `clang`) must be installed on your system

### Build from Source
1. Clone the repository:
   ```bash
   git clone https://github.com/Harshidpatel12/miniscribe.git
   cd miniscribe
   ```
2. Build the executable:
   ```bash
   make build
   ```
   This compiles a portable binary containing custom relative RPATHs linking to the required CGO dependencies into `./bin/miniscribe`.

3. Run the unit tests (which also automatically formats code):
   ```bash
   make test
   ```

4. Format the source code manually:
   ```bash
   make fmt
   ```
