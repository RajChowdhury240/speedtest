# speedtest

#### Animated terminal speedtest. Go + Bubble Tea TUI.

Streams live ping, jitter, download, and upload from the nearest Ookla server with gradient bars and stage transitions.

![demo](demo.gif)

<img width="1606" height="1048" alt="image" src="https://github.com/user-attachments/assets/373e9c3a-d4b3-4fe7-ac73-78c818c10ee6" />


## Install

```bash
go build -o speedtest .
```

Requires Go 1.25+.

## Run

```bash
./speedtest
```

Quits on `q` or `ctrl+c`. Final summary prints after alt-screen exits.

## Layout

```
main.go                    entrypoint
internal/engine/engine.go  speedtest pipeline, streams Update over channel
internal/ui/ui.go          Bubble Tea model, lipgloss styling
```

Engine emits staged `Update` values (`init` → `user` → `servers` → `ping` → `download` → `upload` → `done`). UI consumes channel, renders frame.
