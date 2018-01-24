// +build !windows

// Copyright 2013, Örjan Persson. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package logging

import (
	"bytes"
	"fmt"
	"io"
	"bufio"
	"log"
	"os"
	"sync"
)

type color int

const (
	ColorBlack = iota + 30
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorWhite
)

var (
	colors = []string{
		CRITICAL: ColorSeq(ColorMagenta),
		ERROR:    ColorSeq(ColorRed),
		WARNING:  ColorSeq(ColorYellow),
		NOTICE:   ColorSeq(ColorGreen),
		DEBUG:    ColorSeq(ColorCyan),
	}
	boldcolors = []string{
		CRITICAL: ColorSeqBold(ColorMagenta),
		ERROR:    ColorSeqBold(ColorRed),
		WARNING:  ColorSeqBold(ColorYellow),
		NOTICE:   ColorSeqBold(ColorGreen),
		DEBUG:    ColorSeqBold(ColorCyan),
	}
)

// LogBackend utilizes the standard log module.
type LogBackend struct {
	Logger      *log.Logger
	Color       bool
	ColorConfig []string
}

// NewLogBackend creates a new LogBackend.
func NewLogBackend(out io.Writer, prefix string, flag int) *LogBackend {
	return &LogBackend{Logger: log.New(out, prefix, flag)}
}

// Log implements the Backend interface.
func (b *LogBackend) Log(level Level, calldepth int, rec *Record) error {
	if b.Color {
		col := colors[level]
		if len(b.ColorConfig) > int(level) && b.ColorConfig[level] != "" {
			col = b.ColorConfig[level]
		}

		buf := &bytes.Buffer{}
		buf.Write([]byte(col))
		buf.Write([]byte(rec.Formatted(calldepth + 1)))
		buf.Write([]byte("\033[0m"))
		// For some reason, the Go logger arbitrarily decided "2" was the correct
		// call depth...
		return b.Logger.Output(calldepth+2, buf.String())
	}

	return b.Logger.Output(calldepth+2, rec.Formatted(calldepth+1))
}

// ConvertColors takes a list of ints representing colors for log levels and
// converts them into strings for ANSI color formatting
func ConvertColors(colors []int, bold bool) []string {
	converted := []string{}
	for _, i := range colors {
		if bold {
			converted = append(converted, ColorSeqBold(color(i)))
		} else {
			converted = append(converted, ColorSeq(color(i)))
		}
	}

	return converted
}

func ColorSeq(color color) string {
	return fmt.Sprintf("\033[%dm", int(color))
}

func ColorSeqBold(color color) string {
	return fmt.Sprintf("\033[%d;1m", int(color))
}

func doFmtVerbLevelColor(layout string, level Level, output io.Writer) {
	if layout == "bold" {
		output.Write([]byte(boldcolors[level]))
	} else if layout == "reset" {
		output.Write([]byte("\033[0m"))
	} else {
		output.Write([]byte(colors[level]))
	}
}


// LogBackend utilizes the standard log module.
type FileLogBackend struct {
	Logger      *log.Logger
	file		*os.File
	writer	    *bufio.Writer
	flushLevel  Level
	syncLevel   Level
	lock        sync.Mutex
}

// NewLogBackend creates a new LogBackend.
func NewFileLogBackend(filePath string, prefix string, flag int,
	flushLevel  Level, syncLevel  Level) (*FileLogBackend, error) {
	var backend *FileLogBackend
	backend = nil

	fileHandler, err  := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY,0666)
	if err != nil{
		fmt.Println(err)
	} else {
		w := bufio.NewWriter(fileHandler)

		backend = &FileLogBackend{Logger: log.New(fileHandler, prefix, flag),
			file: fileHandler,
			writer: w,
			flushLevel: flushLevel,
			syncLevel: syncLevel}
	}
	return backend, err
}

// Log implements the Backend interface.
func (b *FileLogBackend) Log(level Level, calldepth int, rec *Record) error {

	b.lock.Lock()
	defer b.lock.Unlock()

	err := b.Logger.Output(calldepth+2, rec.Formatted(calldepth+1))

	if level <= b.syncLevel {
		err = b.file.Sync()
	} else if level >= b.flushLevel && level < DEBUG {
		err = b.writer.Flush()
	}
	return err
}
