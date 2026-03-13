// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pulls

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"

	"code.gitea.io/tea/cmd/flags"
	"code.gitea.io/tea/modules/api"
	"code.gitea.io/tea/modules/context"
	"code.gitea.io/tea/modules/utils"
	"github.com/urfave/cli/v3"
)

// CmdPullsImport imports a GitHub PR into a mirror repository
var CmdPullsImport = cli.Command{
	Name:    "import",
	Aliases: []string{"import-pr"},
	Usage:   "Import a GitHub PR into a mirror repository",
	Description: `Import a specific pull request from the upstream GitHub repository into
a Gitea mirror. The PR will be continuously synced on each mirror sync interval.

The repository must be a mirror of a GitHub repository. If no GitHub token is
embedded in the mirror's remote URL, the server will attempt to use the gh CLI
to obtain one.

Examples:
  tea pulls import 42
  tea pulls import 42 --repo owner/mirror-repo`,
	ArgsUsage: "<github PR number>",
	Action: func(_ stdctx.Context, cmd *cli.Command) error {
		ctx := context.InitCommand(cmd)
		ctx.Ensure(context.CtxRequirement{RemoteRepo: true})

		if cmd.NArg() < 1 {
			return fmt.Errorf("must specify a GitHub PR number")
		}
		idx, err := utils.ArgToIndex(cmd.Args().First())
		if err != nil {
			return err
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/mirror-sync/pr/%d", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("POST", endpoint, nil, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != 200 {
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		var pr struct {
			Number int64  `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
			HTMLURL string `json:"html_url"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.Unmarshal(body, &pr); err != nil {
			return err
		}

		fmt.Printf("Imported PR #%d: %s\n", pr.Number, pr.Title)
		fmt.Printf("  State:  %s\n", pr.State)
		fmt.Printf("  Author: %s\n", pr.User.Login)
		if pr.HTMLURL != "" {
			fmt.Printf("  URL:    %s\n", pr.HTMLURL)
		}

		return nil
	},
	Flags: flags.AllDefaultFlags,
}
