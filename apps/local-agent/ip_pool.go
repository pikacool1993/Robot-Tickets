package main

import "robot/apps/local-agent/internal/ippool"

func (a *App) GetIPPools() (ippool.View, error) { return a.service.GetIPPools() }
func (a *App) SaveIPPool(k string, direct, include bool, threshold, cooldown int) error {
	return a.service.SaveIPPool(k, direct, include, threshold, cooldown)
}
func (a *App) ImportIPPool(k, raw string) ([]string, error) { return a.service.ImportIPPool(k, raw) }
func (a *App) UpdateIPPoolEntry(k, id, action, line string) error {
	return a.service.UpdateIPPoolEntry(k, id, action, line)
}
func (a *App) RandomizeTaskIP(id int64) (string, error) { return a.service.RandomizeTaskIP(id) }
func (a *App) TestIPProxy(k, id string) error           { return a.service.TestIPProxy(k, id) }
