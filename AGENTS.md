# Configuration

- Access environment variables and command line arguments in main.go

# Style

- Use the linux kernel guidelines for commenting insofar as they are applicable to Go (e.g. avoid stating the obvious)
- Use `any` instead of `interface{}` and in general use modern Go
- Make liberal use of line breaks; don't try to stuff structs onto one line

# Error handling

- Wrap errors when passing them back up the stack
- Do not silently ignore errors
- Be defensive with external I/O
- Do not be defensive with internal state (e.g. nil pointers, empty strings)
  - It is the responsibility of the caller to make sure internal objects and parameters are initialized before calling a function that uses them.
  - Let it crash or panic.

# Editing

- Do the simplest thing that works, but no simpler
- Do not mix task categories
  - If asked to make a logic change, do not reorganise or refactor code
  - If asked to refactor code, do not make logic or functionality changes

# Documentation

- Ensure that new command line options and env vars are documented in the CLI help and the README.md.

# Building and linting

- Build with `go build -o voxinput .`
- Lint with `go vet .`
- After updates to verstion.txt, the Nix flake or go.mod build with `nix build .`
  - If vendor hash errors are found, use `fakeHash` then `nix build .` to get the correct hash

# Running

- Use the private scripts in the bringup/ directory
- Suggest creating bringing scripts if they do not exist already
- Example of a bringup script for realtime transcription
```sh
#!/bin/sh
export OPENAI_BASE_URL=http://localai-host:8081/v1 OPENAI_WS_BASE_URL=ws://localai-host:8081/v1/realtime VOXINPUT_TRANSCRIPTION_MODEL=whisper-large-turbo 
export VOXINPUT_PROMPT="VoxInput LocalAI ROS2 LLM NixOS struct env var"
./voxinput listen
```
- Example of a bringup script for transcribing from a monitor device
```sh
#!/bin/sh
OPENAI_BASE_URL=http://ledbx:8081/v1 OPENAI_WS_BASE_URL=ws://ledbx:8081/v1/realtime VOXINPUT_TRANSCRIPTION_MODEL=whisper-large-turbo VOXINPUT_CAPTURE_DEVICE="Monitor of iFi (by AMR) HD USB Audio Analog Stereo" ./voxinput listen --output-file /tmp/transcript.txt
```
