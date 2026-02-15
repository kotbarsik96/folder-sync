package main

import (
	"folder-sync/toast"
)

type AppNotifications struct {
	AppID string
}

var appNotification = AppNotifications{
	AppID: "kb96-folder-sync",
}

func (fsn AppNotifications) Push(notification toast.Notification) {
	err := notification.Push()
	if err != nil {
		logfile.Writef("Ошибка при отправке уведомления: %v", err)
	}
}

func (fsn AppNotifications) AppStarted() {
	fsn.Push(toast.Notification{
		AppID:   fsn.AppID,
		Title:   "Синхронизация",
		Message: "Процесс отслеживания синхронизируемых папок запущен",
	})
}

func (fsn AppNotifications) Synchronized() {
	fsn.Push(toast.Notification{
		AppID:   fsn.AppID,
		Title:   "Синхронизация завершена",
		Message: "Папки синхронизированы",
	})
}
