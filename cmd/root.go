package cmd

import (
	"context"
	"fmt"

	"github.com/merisssas/Bot/cmd/upload"
	"github.com/merisssas/Bot/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "Teleload",
	Short: "Teleload",
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
