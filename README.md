# VoxInput

Transcribe input from your microphone and turn it into key presses on a virtual keyboard. This allows you to use speech-to-text on any application or window system in Linux. In fact you can use it on the system console.

<p align="center">
  <img src="https://github.com/user-attachments/assets/c3cc5a16-b346-4b01-ae8e-fdbcfb1920f3" alt="VoxInput Logo" width="400">
</p>

VoxInput is meant to be used with [LocalAI](https://localai.io), but it will function with any OpenAI compatible API that provides the transcription endpoint or realtime API.

## Features

- **Speech-to-Text Daemon**: Runs as a background process to listen for signals to start or stop recording audio.
- **Audio Capture**: Records audio from the microphone or any other device, including audio you are listening to.
- **Transcription**: Converts recorded audio into text using a local or remote transcription service.
- **Text Automation**: Simulates typing the transcribed text into an application using [`dotool`](https://git.sr.ht/~geb/dotool).
- **Voice Activity Detection**: In realtime mode VoxInput uses VAD to detect speech segments and automatically transcribe them.
- **Visual Notification**: In realtime mode, a GUI notification informs you when recording (VAD) has started or stopped.

[![Usage and Installation video](https://i.ytimg.com/vi/bbZ_9-Uzp78/hqdefault.jpg)](https://youtu.be/bbZ_9-Uzp78)

## Requirements

- `dotool` (for simulating keyboard input)
- `OPENAI_API_KEY` or `VOXINPUT_API_KEY`: Your OpenAI API key for Whisper transcription. If you have a local instance with no key, then just leave it unset.
- `OPENAI_BASE_URL` or `VOXINPUT_BASE_URL`: The base URL of the OpenAI compatible API server: defaults to `http://localhost:8080/v1`
- `XDG_RUNTIME_DIR`: Required for PID and state files in `$XDG_RUNTIME_DIR`.
- `VOXINPUT_LANG`: Language code for transcription (defaults to empty).
- `VOXINPUT_TRANSCRIPTION_MODEL`: Transcription model (default: `whisper-1`).
- `VOXINPUT_TRANSCRIPTION_TIMEOUT`: Timeout duration (default: `30s`).
- `VOXINPUT_SHOW_STATUS`: Show GUI notifications (`yes`/`no`, default: `yes`).
- `VOXINPUT_CAPTURE_DEVICE`: Specific audio capture device name (run `voxinput devices` to list).
- `VOXINPUT_OUTPUT_FILE`: Path to save the transcribed text to a file instead of typing it with dotool.

**Note**: `VOXINPUT_` vars take precedence over `OPENAI_` vars.
Unless you don't mind running VoxInput as root, then you also need to ensure the following is setup for `dotool`

- Your user is in the `input` user group
- You have the following udev rule

```
KERNEL=="uinput", GROUP="input", MODE="0620", OPTIONS+="static_node=uinput"
```

This can be set in your NixOS config as follows
```nix
services.udev.extraRules = ''
KERNEL=="uinput", GROUP="input", MODE="0620", OPTIONS+="static_node=uinput"
'';
```

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/richiejp/VoxInput.git
   cd VoxInput
   ```

2. Build the project:
   ```bash
   go build -o voxinput
   ```

3. Ensure `dotool` is installed on your system and it can make key presses.

4. It makes sense to bind the `record` and `write` commands to keys using your window manager. For instance in my Sway config I have the following

```
bindsym $mod+Shift+t exec voxinput record
bindsym $mod+t exec voxinput write
```

Alternatively you can use the Nix flake.

## Usage

The `LANG` and `VOXINPUT_LANG` environment variables are used to tell the transcription service which language to use.
For multi-lingual use set `VOXINPUT_LANG` to an empty string.

The pop-up window showing when recording has begun can be disabled by setting `VOXINPUT_SHOW_STATUS=no` or `--no-show-status`.

### Commands

- **`listen`**: Start speech to text daemon.
  - `--replay`: Play the audio just recorded for transcription (non-realtime mode only).
  - `--no-realtime`: Use the HTTP API instead of the realtime API; disables VAD.
  - `--no-show-status`: Don't show when recording has started or stopped.
  - `--output-file <path>`: Save transcript to file instead of typing.
  ```bash
  ./voxinput listen
  ```

- **`record`**: Tell existing listener to start recording audio. In realtime mode it also begins transcription.
  ```bash
  ./voxinput record
  ```

- **`write`** or **`stop`**: Tell existing listener to stop recording audio and begin transcription if not in realtime mode. `stop` alias makes more sense in realtime mode.
  ```bash
  ./voxinput write
  ```

- **`toggle`**: Toggle recording on/off (start recording if idle, stop if recording).
  ```bash
  ./voxinput toggle
  ```

- **`status`**: Show whether the server is listening and if it's currently recording.
  ```bash
  ./voxinput status
  ```

- **`devices`**: List capture devices.
  ```bash
  ./voxinput devices
  ```

- **`help`**: Show help message.
  ```bash
  ./voxinput help
  ```

- **`ver`**: Print version.
  ```bash
  ./voxinput ver
  ```

### Example Realtime Workflow

1. Start the daemon in a terminal window:
   ```bash
   OPENAI_BASE_URL=http://ai.local:8081/v1 OPENAI_WS_BASE_URL=ws://ai.local:8081/v1/realtime ./voxinput listen
   ```

2. Select a text box you want to speak into and use a global shortcut to run the following
   ```bash
   ./voxinput record
   ```

3. Begin speaking, when you pause for a second or two your speach will be transcribed and typed into the active application.

4. Send a signal to stop recording
   ```bash
   ./voxinput stop
   ```

### Example Workflow

1. Start the daemon in a terminal window:
   ```bash
   OPENAI_BASE_URL=http://ai.local:8081/v1 ./voxinput listen --no-realtime
   ```

2. Select a text box you want to speak into and use a global shortcut to run the following
   ```bash
   ./voxinput record
   ```

3. After speaking, send a signal to stop recording and transcribe:
   ```bash
   ./voxinput write
   ```

4. The transcribed text will be typed into the active application.

### Example Workflow: Transcribing an Online Meeting or Video Stream

To create a transcript of an online meeting or video stream by capturing system audio:

1. List available capture devices:

   ```bash
   ./voxinput devices
   ```

   Identify the monitor device, e.g., "Monitor of Built-in Audio Analog Stereo".

2. Start the daemon specifying the device and output file:

   ```bash
   VOXINPUT_CAPTURE_DEVICE="Monitor of Built-in Audio Analog Stereo" ./voxinput listen --output-file meeting_transcript.txt
   ```

   Note: Add `--no-realtime` if you prefer the HTTP API.

3. Start recording:

   ```bash
   ./voxinput record
   ```

4. Play your online meeting or video stream; the system audio will be captured.

5. Stop recording:

   ```bash
   ./voxinput stop
   ```

6. The transcript is now in `meeting_transcript.txt`.

### Quick start with LocalAI

1. Follow https://localai.io/installation/ to install LocalAI, the simplest way is using Docker:

```bash
docker run -p 8080:8080 --name local-ai -ti localai/localai:latest
```

2. Open http://localhost:8080 in your browser to access the LocalAI web interface and install the whisper-1 and silero-vad-ggml models.

3. Test out VoxInput:

```
VOXINPUT_TRANSCRIPTION_MODEL=whisper-1 VOXINPUT_TRANSCRIPTION_TIMEOUT=30s voxinput listen
voxinput record && wait 30s && voxinput write
```

### Displaying recording status

The realtime mode has a UI to display various actions being taken by VoxInput. However you can also read the status from the status file or using the status command, then display it via your desktop manager (e.g. waybar). For an example see the [PR which added it](https://github.com/richiejp/VoxInput/pull/26).

## TODO

- [x] Put playback behind a debug switch
- [x] Create a release
- [x] Realtime Transcription
- [x] GUI and system tray
- [x] Voice detection and activation (partial, see below)
- [ ] Code words to start and stop transcription
- [ ] Allow user to describe a button they want to press (requires submitting screen shot and transcription to LocalAGI)

## Signals

- `SIGUSR1`: Start recording audio.
- `SIGUSR2`: Stop recording and transcribe audio.
- `SIGTERM`: Stop the daemon.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [malgo](https://github.com/gen2brain/malgo) for audio handling.
- [go-openai](https://github.com/sashabaranov/go-openai) for OpenAI API integration.
- [numen](https://git.sr.ht/~geb/numen) and dotool, I did consider modifying numen to use LocalAI, but decided to go with a new tool for now.

---

Feel free to contribute or report issues! 😊
