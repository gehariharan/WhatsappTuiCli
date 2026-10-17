package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"whatsappsync/internal/config"
	"whatsappsync/internal/localfs"
	"whatsappsync/internal/storage"
	"whatsappsync/internal/tui"
)

func (a *App) tuiAction(offline bool) action {
	return func(ctx context.Context, cfg config.Config, store *storage.Store, _ *slog.Logger) (result error) {
		if !a.Interactive {
			return fmt.Errorf("the TUI requires an interactive terminal; use wasync help for scriptable commands")
		}
		if offline {
			return a.withAPI(ctx, cfg, store, nil, func(ctx context.Context, url string) error {
				session := tui.NewSession(ctx, store, nil)
				session.APIURL = url
				return tui.Run(ctx, session, a.In, a.Out)
			})
		}
		file, err := localfs.OpenFile(filepath.Join(cfg.DataDir, "wasync.log"))
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, file.Close()) }()
		if _, err = file.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		log := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo}))
		clientCtx, cancel := context.WithCancel(ctx)
		client, err := a.OpenClient(clientCtx, filepath.Join(cfg.DataDir, "session.db"), log)
		if err != nil {
			cancel()
			return err
		}
		defer func() { cancel(); result = errors.Join(result, client.Close()) }()
		live, ok := client.(tui.Client)
		if !ok {
			return fmt.Errorf("this client does not implement capture")
		}
		if err = live.EnableCapture(clientCtx, store); err != nil {
			return err
		}
		return a.withAPI(clientCtx, cfg, store, live.Status, func(ctx context.Context, url string) error {
			session := tui.NewSession(ctx, store, live)
			session.APIURL = url
			return tui.Run(ctx, session, a.In, a.Out)
		})
	}
}
