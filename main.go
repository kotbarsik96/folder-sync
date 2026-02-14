package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/fsnotify.v1"
)

type watcherWithData struct {
	w *fsnotify.Watcher
	// список папок, на которые добавлен watcher
	dirnames map[string]bool
}

// добавляет watcher на name, если name отсутствует в списке dirnames
func (wwd *watcherWithData) Add(dirname string) error {
	_, added := wwd.dirnames[dirname]
	if added {
		return nil
	}

	err := wwd.w.Add(dirname)
	if err != nil {
		logfile.Writef("Не удалось начать отслеживание папки %s: %v", dirname, err)
	}
	logfile.Writef("Начало отслеживания папки %s", dirname)

	wwd.dirnames[dirname] = true
	return err
}

func NewWatcher() *watcherWithData {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Не удалось создать watcher: %v", err)
	}
	return &watcherWithData{watcher, make(map[string]bool)}
}

type eventContext struct {
	ctx           context.Context
	cancelCtxFunc context.CancelFunc
	mu            sync.Mutex
	done          chan bool
}

func main() {
	flag.Parse()
	start()
}

func start() {
	if *rootDirname == "" {
		log.Fatal("Не указана отслеживаемая директория (src)")
	}

	if len(destDirnames()) < 1 {
		log.Fatal("Не указаны папки для синхронизации (dest)")
	}
	for _, d := range destDirnames() {
		_, err := filepath.Rel(*rootDirname, d)
		if err == nil {
			log.Fatalf("Папка для синхронизации не должна находиться внутри основной папки: %v внутри %v", d, *rootDirname)
		}

		_, err = filepath.Rel(d, *rootDirname)
		if err == nil {
			log.Fatalf("Основная папка не должна находиться внутри папки для синхронизации: %v внутри %v", *rootDirname, d)
		}
	}

	watcher := NewWatcher()
	defer watcher.w.Close()

	done := make(chan bool)

	go listenToEventsAndErrors(watcher)
	filepath.Walk(*rootDirname, func(path string, info fs.FileInfo, _ error) error {
		if info.IsDir() {
			watcher.Add(path)
		}
		return nil
	})

	<-done
}

func listenToEventsAndErrors(watcher *watcherWithData) {
	var ctx eventContext = eventContext{done: make(chan bool, 1)}

	for {
		select {
		case event := <-watcher.w.Events:
			go handleEvent(&event, &ctx, watcher)
		case err := <-watcher.w.Errors:
			logfile.Writef("Ошибка отслеживания события: %v", err)
		}
	}
}

func handleEvent(event *fsnotify.Event, ectx *eventContext, watcher *watcherWithData) {
	ectx.mu.Lock()

	// добавить в отслеживаемые, если каталог. Если уже добавлен - ничего не будет сделано
	if event.Op == fsnotify.Create || event.Op == fsnotify.Write || event.Op == fsnotify.Rename {
		info, _ := os.Stat(event.Name)
		if info != nil && info.IsDir() {
			watcher.Add(event.Name)
		}
	}

	if isExcludedPath(event.Name) {
		ectx.mu.Unlock()
		return
	}

	logfile.Writef("Произошло изменение: %s (%s)", event.Name, event.Op)

	if ectx.cancelCtxFunc != nil {
		ectx.cancelCtxFunc()
		// ждёт, пока контекст завершится полностью, чтобы не переопределить его раньше времени
		<-ectx.done
	}

	ticker := time.NewTicker(1 * time.Second)
	ectx.ctx, ectx.cancelCtxFunc = context.WithCancel(context.Background())

	ectx.mu.Unlock()

	// отсчёт таймера
	for countdown := TIMER_PERIOD_SECONDS; countdown > 0; countdown-- {
		select {
		case <-ectx.ctx.Done():
			ticker.Stop()
			ectx.done <- true
			return
		case <-ticker.C:
			fmt.Printf("%d...\n", countdown)
		}
	}

	// запуск после отсчёта
	startSync(ectx)
}

func startSync(ectx *eventContext) {
	defer logfile.WriteToFiles()
	logfile.Writef("Запуск синхронизации: %v", time.Now())

	removeDestDirnames()

	filepath.Walk(*rootDirname, func(path string, info fs.FileInfo, err error) error {
		if isExcludedPath(path) {
			return nil
		}

		if ectx.ctx.Err() != nil {
			logfile.Writef("SkipAll: контекст завершён (начало [Walk], итерация %s)", path)
			return filepath.SkipAll
		}

		relPath, err := filepath.Rel(*rootDirname, path)
		if err != nil {
			logfile.Writef("Не удалось получить путь: %v", err)
			return filepath.SkipDir
		}

		var wg sync.WaitGroup
		for _, destDirname := range destDirnames() {
			ddir := destDirname
			wg.Add(1)

			go func() {
				defer wg.Done()
				targetPath := filepath.Join(ddir, relPath)

				if info.IsDir() {
					err = os.MkdirAll(targetPath, info.Mode())
					if err != nil {
						logfile.Writef("Не удалось создать dest-директорию: %v", err)
					}
				} else {
					srcFile, err := os.Open(path)
					if err != nil {
						logfile.Writef("Не удалось открыть src-файл: %v", err)
						srcFile.Close()
						return
					}

					destFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
					if err != nil {
						logfile.Writef("Не удалось создать dest-file: %v", err)
						srcFile.Close()
						destFile.Close()
						return
					}

					logfile.Writef("Начало копирования файла: %s", targetPath)
					doneCopying := make(chan error, 1)
					go func() {
						// копирование будет остановлено закрытием файлов, если контекст завершится
						_, err = io.Copy(destFile, srcFile)
						doneCopying <- err
					}()

					select {
					case <-ectx.ctx.Done():
						// destFile и srcFile будут закрыты. Это остановит копирование, запущенное в горутине выше
						srcFile.Close()
						destFile.Close()
					case err := <-doneCopying:
						if err != nil {
							srcFile.Close()
							destFile.Close()
							logfile.Writef("Ошибка при копировании в %s: %v", targetPath, err)
						} else {
							os.Chmod(targetPath, info.Mode())
							srcFile.Close()
							destFile.Close()
							logfile.Writef("Синхронизировано: %s", targetPath)
						}
					}
				}
			}()
		}
		wg.Wait()

		if ectx.ctx.Err() != nil {
			logfile.Writef("SkipAll: контекст завершён (конец [Walk], итерация %s)", path)
			return filepath.SkipAll
		}

		return nil
	})

	ectx.done <- true

	if ectx.ctx.Err() == nil {
		logfile.Writef("Синхронизация завершена полностью: %v", time.Now())
	}
}

// не обрабатывать изменения в файлах, находящихся в excludedPaths
func isExcludedPath(path string) bool {
	isExcluded := false
	for _, ep := range excludedPaths {
		pathRel, err := filepath.Rel(*rootDirname, path)
		epRel, _ := filepath.Rel(*rootDirname, filepath.Join(*rootDirname, ep))
		if pathRel == epRel && err == nil {
			isExcluded = true
			break
		}
	}
	return isExcluded
}

func removePathFromDests(path string) {
	relPath, err := filepath.Rel(*rootDirname, path)
	if err != nil {
		logfile.Writef("Не удалось создать путь для удалённого файла/директории %s: %v", path, err)
		return
	}

	var wg sync.WaitGroup
	for _, destDirname := range destDirnames() {
		wg.Add(1)
		dd := destDirname

		go func() {
			defer wg.Done()
			targetPath := filepath.Join(dd, relPath)

			err = os.RemoveAll(targetPath)
			if err != nil {
				logfile.Writef("Не удалось удалить файл/директорию: %v", err)
			}
		}()
	}
	wg.Wait()
}

func removeDestDirnames() {
	var wg sync.WaitGroup
	for _, destdirname := range destDirnames() {
		dd := destdirname
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := os.RemoveAll(dd)
			if err != nil {
				logfile.Writef("Не удалось удалить каталог %s: %v", dd, err)
			}
		}()
	}
	wg.Wait()
}
