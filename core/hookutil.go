package core

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/charmbracelet/log"
)

func ExecCommandString(ctx context.Context, cmd string) error {
	if cmd == "" {
		return nil
	}
	logger := log.FromContext(ctx)
	args, err := parseCommandArgs(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse hook command: %w", err)
	}
	if len(args) == 0 {
		return nil
	}
	execCmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err = execCmd.Run()
	logCommandOutput(logger, "hook stdout", stdout.String())
	logCommandOutput(logger, "hook stderr", stderr.String())
	return err
}

func parseCommandArgs(cmd string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for _, r := range cmd {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf("unfinished escape sequence in hook command")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in hook command")
	}
	flush()
	return args, nil
}

func logCommandOutput(logger *log.Logger, label string, content string) {
	const maxLogSize = 4096
	if content == "" {
		return
	}
	if len(content) > maxLogSize {
		logger.Debugf("%s (truncated): %s", label, content[:maxLogSize])
		return
	}
	logger.Debugf("%s: %s", label, content)
}
