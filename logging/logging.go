package logging

import (
	"fmt"
	"io"
	"log"
	"os"
)

var logger = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)

func Init(logPath string) (*os.File, error) {
	file, errOpen := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if errOpen != nil {
		return nil, errOpen
	}
	logger.SetOutput(io.MultiWriter(os.Stdout, file))
	return file, nil
}

func Infof(format string, args ...interface{}) {
	logf("INFO", format, args...)
}

func Warnf(format string, args ...interface{}) {
	logf("WARN", format, args...)
}

func Errorf(format string, args ...interface{}) {
	logf("ERROR", format, args...)
}

func Fatalf(format string, args ...interface{}) {
	logf("FATAL", format, args...)
	os.Exit(1)
}

func logf(level, format string, args ...interface{}) {
	message := fmt.Sprintf("["+level+"] "+format, args...)
	_ = logger.Output(4, message)
}
