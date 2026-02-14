package main

import (
	"flag"
	"strings"
)

const TIMER_PERIOD_SECONDS = 5

const LOGGER_FILENAME = "_sync-logs.txt"

var (
	rootDirname      = flag.String("src", "", "Папка, содержимое которой будет отслеживаться и копироваться в другие")
	destDirnamesFlag = flag.String("dst", "", "Папки, в которые будет выполняться копирование из src. Перечисляется через пробел: C:\\Test D:\\Test")
)

func destDirnames() []string {
	var s []string
	for _, dirname := range strings.Split(*destDirnamesFlag, " ") {
		if strings.TrimSpace(dirname) != "" {
			s = append(s, dirname)
		}
	}
	return s
}

var logfile *logger = newLogger()

// пути/названия файлов (относительно rootDirname), изменения которых будут проигнорированы и не запустят синхронизацию
var excludedPaths []string = []string{
	LOGGER_FILENAME,
}
