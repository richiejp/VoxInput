# VoxInput

Transcribe input from your microphone and turn it into key presses on a virtual keyboard. This allows you to use speech-to-text on any application or window system in Linux. In fact you can use it on the system console.

<p align="center">
  <img src="https://github.com/user-attachments/assets/bc1de2af-e07b-4460-a522-b140a041a3db" alt="VoxInput Robot Mascot" width="400">
</p>

VoxInput is meant to be used with [LocalAI](https://localai.io), but it will function with any OpenAI compatible API that provides the transcription endpoint or realtime API.

## Features

- **Speech-to-Text Daemon**: Runs as a background process to listen for signals to start or stop recording audio.
- **Audio Capture and Playback**: Records audio from the microphone and plays it back for verification.
- **Transcription**: Converts recorded audio into text using a local or remote transcription service.
- **Text Automation**: Simulates typing the transcribed text into an application using [`dotool`](https://git.sr.ht/~geb/dotool).
- **Voice Activity Detection**: In realtime mode VoxInput uses VAD to detect speech segments and automatically transcribe them.
- **Visual Notification**: In realtime mode, a GUI notification informs you when recording (VAD) has started or stopped.

[![Usage and Installation video](https://i.ytimg.com/vi/bbZ_9-Uzp78/hqdefault.jpg)](https://youtu.be/bbZ_9-Uzp78)

## Requirements

- `dotool` (for simulating keyboard input)
- `OPENAI_API_KEY` or `VOXINPUT_API_KEY`: Your OpenAI API key for Whisper transcription. If you have a local instance with no key, then just leave it unset.
- `OPENAI_BASE_URL` or `VOXINPUT_BASE_URL`: The base URL of the OpenAI compatible API server: defaults to `http://localhost:8080/v1`
- `OPENAI_WS_BASE_URL` or `VOXINPUT_WS_BASE_URL`: The base URL of the realtime websocket API: defaults to `ws://localhost:8080/v1/realtime`
- OpenAI Realtime API support - VoxInput's realtime mode with VAD requires a [websocket endpoint that support's OpenAI's realtime API in transcription only mode](https://github.com/mudler/LocalAI/pull/5392). You can disable realtime mode with `--no-realtime`.

Note that the VoxInput env vars take precedence over the OpenAI ones.

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
   git clone https://github.com/yourusername/VoxInput.git
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

- **`listen`**: Starts the speech-to-text daemon.
  ```bash
  ./voxinput listen
  ```

- **`record`**: Sends a signal to the daemon to start recording audio then exits. In realtime mode this will start transcription.
  ```bash
  ./voxinput record
  ```

- **`write`** or **`stop`**: Sends a signal to the daemon to stop recording. When not in realtime mode this triggers transcription.
  ```bash
  ./voxinput write
  ```

- **`help`**: Displays help information.
  ```bash
  ./voxinput help
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

## Limitations

- Uses the default audio input, make sure you have the device you want to use set as the default on your system.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [malgo](https://github.com/gen2brain/malgo) for audio handling.
- [go-openai](https://github.com/sashabaranov/go-openai) for OpenAI API integration.
- [numen](https://git.sr.ht/~geb/numen) and dotool, I did consider modifying numen to use LocalAI, but decided to go with a new tool for now.

---

Feel free to contribute or report issues! 😊
