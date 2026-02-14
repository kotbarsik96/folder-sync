package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type logger struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

// записать str в лог
func (lf *logger) Write(str string) {
	lf.mu.Lock()
	fmt.Println(str)
	fmt.Fprintf(&lf.buf, "%s\n", str)
	lf.mu.Unlock()
}

// записать в лог с форматированием
func (lf *logger) Writef(format string, a ...any) {
	lf.Write(fmt.Sprintf(format, a...))
}

// перенести информацию из буфера в файлы
func (lf *logger) WriteToFiles() {
	lf.mu.Lock()
	defer lf.mu.Unlock()

	var allDirs []string = []string{*rootDirname}
	allDirs = append(allDirs, destDirnames()...)

	var wg sync.WaitGroup
	for _, rd := range allDirs {
		wg.Add(1)
		rdpath := rd
		go func() {
			defer wg.Done()
			lfile, err := os.OpenFile(filepath.Join(rdpath, LOGGER_FILENAME), os.O_RDWR|os.O_CREATE, 0666)
			if err != nil {
				// нотификацию, что не удалось создать лог-файл в таком-то rd
				fmt.Printf("Не удалось создать logfile: %v", err)
			}
			defer lfile.Close()
			lfile.Write(lf.buf.Bytes())
		}()
	}
	wg.Wait()
}

func newLogger() *logger {
	return &logger{}
}
