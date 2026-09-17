package source

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nxadm/tail"
	"github.com/somewearlabs/chainsaw/internal/config"
)

type Line struct {
	Label   string
	Content string
	IsErr   bool // came from stderr
}

// Start fans out all sources in the config into a single channel.
// The channel is closed when ctx is cancelled and all goroutines have exited.
func Start(ctx context.Context, lc *config.LogConfig) (<-chan Line, error) {
	out := make(chan Line, 512)

	var fns []func()
	for _, s := range lc.Sources {
		s := s // capture
		switch s.Type {
		case "file":
			paths, err := resolveGlob(s.Path)
			if err != nil {
				return nil, fmt.Errorf("resolving %q: %w", s.Path, err)
			}
			if len(paths) == 0 {
				// Still add a waiter so we don't block on an empty source set;
				// the file may appear later but for now we warn once.
				fmt.Printf("warning: no files matched %q\n", s.Path)
				continue
			}
			for _, p := range paths {
				p := p
				label := s.Label
				if label == "" {
					label = filepath.Base(p)
				}
				fns = append(fns, func() { tailFile(ctx, p, label, out) })
			}
		case "command":
			label := s.Label
			if label == "" {
				label = cmdLabel(s.Command)
			}
			fns = append(fns, func() { runCommand(ctx, s.Command, label, out) })
		default:
			return nil, fmt.Errorf("unknown source type %q", s.Type)
		}
	}

	if len(fns) == 0 {
		return nil, fmt.Errorf("no sources could be started")
	}

	done := make(chan struct{}, len(fns))
	for _, fn := range fns {
		fn := fn
		go func() {
			defer func() { done <- struct{}{} }()
			fn()
		}()
	}

	go func() {
		remaining := len(fns)
		for remaining > 0 {
			<-done
			remaining--
		}
		close(out)
	}()

	return out, nil
}

func resolveGlob(pattern string) ([]string, error) {
	// Expand home dir
	if strings.HasPrefix(pattern, "~/") {
		home, err := homeDir()
		if err != nil {
			return nil, err
		}
		pattern = filepath.Join(home, pattern[2:])
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if matches == nil {
		return []string{pattern}, nil // treat as literal path, tail will retry
	}
	return matches, nil
}

func tailFile(ctx context.Context, path, label string, out chan<- Line) {
	t, err := tail.TailFile(path, tail.Config{
		Follow:    true,
		ReOpen:    true, // handle log rotation
		MustExist: false,
		Poll:      true, // use polling for cross-platform compat
		Location:  &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
	})
	if err != nil {
		select {
		case out <- Line{Label: label, Content: fmt.Sprintf("error: %v", err), IsErr: true}:
		case <-ctx.Done():
		}
		return
	}
	defer t.Stop()

	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			if line.Err != nil {
				select {
				case out <- Line{Label: label, Content: fmt.Sprintf("tail error: %v", line.Err), IsErr: true}:
				case <-ctx.Done():
					return
				}
				continue
			}
			select {
			case out <- Line{Label: label, Content: line.Text}:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func runCommand(ctx context.Context, command, label string, out chan<- Line) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		err := streamCommand(ctx, command, label, out)
		if err == nil {
			// Clean exit — do not restart
			return
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case out <- Line{Label: label, Content: fmt.Sprintf("[cmd exited: %v — restarting in 2s]", err), IsErr: true}:
		case <-ctx.Done():
			return
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func streamCommand(ctx context.Context, command, label string, out chan<- Line) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	streamLines := func(r io.Reader, isErr bool) {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			select {
			case out <- Line{Label: label, Content: sc.Text(), IsErr: isErr}:
			case <-ctx.Done():
				return
			}
		}
	}

	go streamLines(stdout, false)
	go streamLines(stderr, true)

	return cmd.Wait()
}

func cmdLabel(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "cmd"
	}
	return filepath.Base(parts[0])
}

func homeDir() (string, error) {
	return os.UserHomeDir()
}
