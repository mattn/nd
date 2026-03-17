package main

import (
	"io"
	"os/exec"
	"sync"
)

// Player wraps mpv for audio playback.
// Uses mpv's stdin command interface to control playback,
// which works across WSL → Windows boundaries.
type Player struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mpvPath string
	playing bool
	doneCh  chan struct{}
}

func NewPlayer(mpvPath string) *Player {
	return &Player{mpvPath: mpvPath}
}

func (p *Player) Play(url string) error {
	p.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()

	cmd := exec.Command(p.mpvPath,
		"--no-video",
		//"--no-terminal",
		"--really-quiet",
		url,
	)
	setSysProcAttr(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return err
	}
	p.cmd = cmd
	p.stdin = stdin
	p.playing = true
	doneCh := make(chan struct{})
	p.doneCh = doneCh

	go func() {
		_ = cmd.Wait()
		p.mu.Lock()
		if p.cmd == cmd {
			p.playing = false
			p.cmd = nil
			p.stdin = nil
		}
		p.mu.Unlock()
		close(doneCh)
	}()
	return nil
}

func (p *Player) Stop() {
	p.mu.Lock()
	cmd := p.cmd
	stdin := p.stdin
	doneCh := p.doneCh
	p.cmd = nil
	p.stdin = nil
	p.playing = false
	p.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Close stdin to signal mpv to exit
		if stdin != nil {
			stdin.Close()
		}
		// Send CTRL-C / SIGINT to gracefully terminate
		_ = terminateProcess(cmd)
		if doneCh != nil {
			<-doneCh
		}
	}
}

func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Done returns a channel that is closed when playback finishes.
func (p *Player) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.doneCh != nil {
		return p.doneCh
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (p *Player) Cleanup() {
	p.Stop()
}
