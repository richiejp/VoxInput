# VoxInput

Transcribe input from your microphone and turn it into key presses on a virtual keyboard. This allows you to use speech-to-text on any application or window system in Linux. In fact you can use it on the system console.

<p align="center">
  <img src="https://github.com/user-attachments/assets/bc1de2af-e07b-4460-a522-b140a041a3db" alt="VoxInput Robot Mascot" width="400">
</p>

VoxInput is meant to be used with [LocalAI](https://localai.io), but it will function with any OpenAI compatible API that provides the transcription endpoint.

## Features

- **Speech-to-Text Daemon**: Runs as a background process to listen for signals to start or stop recording audio.
- **Audio Capture and Playback**: Records audio from the microphone and plays it back for verification.
- **Transcription**: Converts recorded audio into text using a local or remote transcription service.
- **Text Automation**: Simulates typing the transcribed text into an application using [`dotool`](https://git.sr.ht/~geb/dotool).

## Requirements

- `dotool` (for simulating keyboard input)
- `OPENAI_API_KEY` or `VOXINPUT_API_KEY`: Your OpenAI API key for Whisper transcription. If you have a local instance with no key, then just leave it unset.
- `OPENAPI_BASE_URL` or `VOXINPUT_BASE_URL`: The base URL of the OpenAI Whisper API server: defaults to `http://localhost:8080/v1`

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

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Build the project:
   ```bash
   go build -o voxinput
   ```

4. Ensure `dotool` is installed on your system and it can make key presses.

5. It makes sense to bind the `record` and `write` commands to keys using your window manager. For instance in my Sway config I have the following

```
bindsym $mod+Shift+t exec VoxInput record
bindsym $mod+t exec VoxInput write
```

## Usage

### Commands

- **`listen`**: Starts the speech-to-text daemon.
  ```bash
  ./voxinput listen
  ```

- **`record`**: Sends a signal to the daemon to start recording audio then exits.
  ```bash
  ./voxinput record
  ```

- **`write`**: Sends a signal to the daemon to stop recording, transcribe the audio, and simulate typing the text.
  ```bash
  ./voxinput write
  ```

- **`help`**: Displays help information.
  ```bash
  ./voxinput help
  ```

### Example Workflow

1. Start the daemon in a terminal window:
   ```bash
   ./voxinput listen
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

- [ ] Put playback behind a debug switch
- [ ] Create a release
- [ ] Realtime Transcription
- [ ] GUI and system tray
- [ ] Voice detection and activation
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
