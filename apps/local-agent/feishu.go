package main

import "robot/apps/local-agent/internal/feishunotify"

func (a *App) GetFeishuStatus() (feishunotify.View, error) { return a.service.GetFeishuStatus() }
func (a *App) TestFeishu() error                           { return a.service.TestFeishu() }
