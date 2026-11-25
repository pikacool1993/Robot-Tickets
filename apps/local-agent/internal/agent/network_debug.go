package agent

import (
	"context"
	"robot/apps/local-agent/internal/ippool"
	"robot/apps/local-agent/internal/netdebug"
)

func (s *Service) taskNetworkContext(ctx context.Context, id int64) context.Context {
	return netdebug.WithTask(ippool.WithTask(ctx, id), id, func(message string) {
		if err := s.tasks.AppendLog(id, "debug", message); err != nil && s.log != nil {
			s.log.Warn("network task log persistence failed", "task_id", id)
		}
	})
}
