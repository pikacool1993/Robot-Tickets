package main

import (
	_ "embed"
	"robot/apps/local-agent/internal/orderalarm"
)

//go:embed frontend/public/alert.mp3
var orderAlertAudio []byte

func (a *App) GetOrderAlarm() (orderalarm.View, error) { return a.service.GetOrderAlarm() }
func (a *App) SaveOrderAlarm(enabled bool) error       { return a.service.SaveOrderAlarm(enabled) }
func (a *App) StopOrderAlarm() error                   { return a.service.StopOrderAlarm() }
func (a *App) PreviewOrderAlarm() error                { return a.service.PreviewOrderAlarm() }
