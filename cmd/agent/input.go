package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
)

type lineInput interface {
	ReadLine(prompt string) (string, error)
	Close() error
}

type basicLineInput struct {
	reader *bufio.Reader
	out    io.Writer
}

func newBasicLineInput(in io.Reader, out io.Writer) *basicLineInput {
	return &basicLineInput{
		reader: bufio.NewReader(in),
		out:    out,
	}
}

func (b *basicLineInput) ReadLine(prompt string) (string, error) {
	if b.out != nil {
		fmt.Fprint(b.out, prompt)
	}
	line, err := b.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (b *basicLineInput) Close() error { return nil }

type readlineInput struct {
	instance *readline.Instance
}

func newReadlineInput(historyPath string) (*readlineInput, error) {
	if historyPath != "" {
		if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
			return nil, fmt.Errorf("create history dir: %w", err)
		}
	}
	instance, err := readline.NewEx(&readline.Config{
		Prompt:            "> ",
		HistoryFile:       historyPath,
		HistorySearchFold: true,
	})
	if err != nil {
		return nil, err
	}
	return &readlineInput{instance: instance}, nil
}

func (r *readlineInput) ReadLine(prompt string) (string, error) {
	r.instance.SetPrompt(prompt)
	return r.instance.Readline()
}

func (r *readlineInput) Close() error {
	if r == nil || r.instance == nil {
		return nil
	}
	return r.instance.Close()
}

func newLineInput(historyPath string) (lineInput, error) {
	readlineReader, err := newReadlineInput(historyPath)
	if err == nil {
		return readlineReader, nil
	}
	return newBasicLineInput(os.Stdin, os.Stdout), err
}

func printREPLCommands(out io.Writer) {
	if out == nil {
		return
	}
	fmt.Fprintln(out, "commands:")
	for _, cmd := range replCommands {
		fmt.Fprintf(out, "  %s\n", cmd)
	}
}
