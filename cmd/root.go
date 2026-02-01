package cmd

import (
	"context"
	"fmt"

	"github.com/merisssas/bot/cmd/upload"
	"github.com/merisssas/bot/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "saveany-bot",
	Short: "saveany-bot",
	Run:   Run,
}

func init() {
	config.RegisterFlags(rootCmd)
	upload.Register(rootCmd)
}

func Execute(ctx context.Context) {
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Println(err)
	}
}
