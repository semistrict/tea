// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pulls

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"code.gitea.io/tea/cmd/flags"
	"code.gitea.io/tea/modules/api"
	"code.gitea.io/tea/modules/context"
	"code.gitea.io/tea/modules/utils"
	"github.com/urfave/cli/v3"
)

type guidedReviewRequest struct {
	Content string `json:"content"`
}

type guidedReviewResponse struct {
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

// CmdPullsGuidedReview manages guided review for a pull request
var CmdPullsGuidedReview = cli.Command{
	Name:        "guided-review",
	Aliases:     []string{"gr"},
	Usage:       "Manage the guided review for a pull request",
	Description: "Get, set, or delete the guided review document that controls how the diff is presented to reviewers",
	Commands: []*cli.Command{
		&cmdGuidedReviewGet,
		&cmdGuidedReviewSet,
		&cmdGuidedReviewDelete,
		&cmdGuidedReviewListHunks,
	},
}

var cmdGuidedReviewGet = cli.Command{
	Name:      "get",
	Usage:     "Get the guided review for a pull request",
	ArgsUsage: "<pull index>",
	Action: func(_ stdctx.Context, cmd *cli.Command) error {
		ctx := context.InitCommand(cmd)
		ctx.Ensure(context.CtxRequirement{RemoteRepo: true})

		if cmd.NArg() < 1 {
			return fmt.Errorf("must specify a PR index")
		}
		idx, err := utils.ArgToIndex(cmd.Args().First())
		if err != nil {
			return err
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/guided-review", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("GET", endpoint, nil, nil)
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

		var review guidedReviewResponse
		if err := json.Unmarshal(body, &review); err != nil {
			return err
		}

		if review.Content == "" {
			fmt.Println("No guided review set for this pull request.")
			return nil
		}

		fmt.Printf("Guided review (updated %s):\n\n", review.UpdatedAt)
		fmt.Println(review.Content)
		return nil
	},
	Flags: flags.AllDefaultFlags,
}

var cmdGuidedReviewSet = cli.Command{
	Name:  "set",
	Usage: "Set the guided review for a pull request",
	Description: `Set a guided review document for a PR. The document is markdown with
embedded diff-hunk blocks that reference specific hunks.

Read from a file with --from-file or pipe from stdin with --from-file -.

Examples:
  tea pulls guided-review set 1 --from-file review.md
  cat review.md | tea pulls guided-review set 1 --from-file -`,
	ArgsUsage: "<pull index>",
	Action: func(_ stdctx.Context, cmd *cli.Command) error {
		ctx := context.InitCommand(cmd)
		ctx.Ensure(context.CtxRequirement{RemoteRepo: true})

		if cmd.NArg() < 1 {
			return fmt.Errorf("must specify a PR index")
		}
		idx, err := utils.ArgToIndex(cmd.Args().First())
		if err != nil {
			return err
		}

		fromFile := cmd.String("from-file")
		if fromFile == "" {
			return fmt.Errorf("must specify --from-file (use - for stdin)")
		}

		var data []byte
		if fromFile == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(fromFile)
		}
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", fromFile, err)
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			return fmt.Errorf("guided review content is empty")
		}

		reqBody := guidedReviewRequest{Content: content}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/guided-review", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("PUT", endpoint, bytes.NewReader(bodyBytes), nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != 200 {
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
		}

		fmt.Println("Guided review set.")
		return nil
	},
	Flags: append([]cli.Flag{
		&cli.StringFlag{
			Name:  "from-file",
			Usage: "Read guided review markdown from a file (use - for stdin)",
		},
	}, flags.AllDefaultFlags...),
}

type diffHunkInfo struct {
	Index      int    `json:"index"`
	Header     string `json:"header"`
	StartLineL int    `json:"start_line_left"`
	StartLineR int    `json:"start_line_right"`
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
}

type diffFileHunks struct {
	Name  string          `json:"name"`
	Hunks []*diffHunkInfo `json:"hunks"`
}

type diffFileHunksResponse struct {
	Files []*diffFileHunks `json:"files"`
}

var cmdGuidedReviewListHunks = cli.Command{
	Name:      "list-hunks",
	Aliases:   []string{"hunks"},
	Usage:     "List hunks for files in a pull request diff",
	ArgsUsage: "<pull index> [file]",
	Description: `Shows the hunks (sections) for each file in a PR diff with their index,
line range, and change count. Use this to plan the guided review document.

If a file path is given, only show hunks for that file.

Examples:
  tea pulls guided-review list-hunks 2
  tea pulls guided-review list-hunks 2 big_file.go`,
	Action: func(_ stdctx.Context, cmd *cli.Command) error {
		ctx := context.InitCommand(cmd)
		ctx.Ensure(context.CtxRequirement{RemoteRepo: true})

		if cmd.NArg() < 1 {
			return fmt.Errorf("must specify a PR index")
		}
		idx, err := utils.ArgToIndex(cmd.Args().First())
		if err != nil {
			return err
		}

		filterFile := ""
		if cmd.NArg() > 1 {
			filterFile = cmd.Args().Get(1)
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/guided-review/hunks", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("GET", endpoint, nil, nil)
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

		var hunksResp diffFileHunksResponse
		if err := json.Unmarshal(body, &hunksResp); err != nil {
			return err
		}

		for _, file := range hunksResp.Files {
			if filterFile != "" && file.Name != filterFile {
				continue
			}
			fmt.Printf("%s (%d hunks):\n", file.Name, len(file.Hunks))
			for _, h := range file.Hunks {
				fmt.Printf("  [%d] %s  (+%d -%d)\n", h.Index, h.Header, h.Additions, h.Deletions)
			}
			fmt.Println()
		}
		return nil
	},
	Flags: flags.AllDefaultFlags,
}

var cmdGuidedReviewDelete = cli.Command{
	Name:      "delete",
	Aliases:   []string{"rm"},
	Usage:     "Delete the guided review for a pull request",
	ArgsUsage: "<pull index>",
	Action: func(_ stdctx.Context, cmd *cli.Command) error {
		ctx := context.InitCommand(cmd)
		ctx.Ensure(context.CtxRequirement{RemoteRepo: true})

		if cmd.NArg() < 1 {
			return fmt.Errorf("must specify a PR index")
		}
		idx, err := utils.ArgToIndex(cmd.Args().First())
		if err != nil {
			return err
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/guided-review", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("DELETE", endpoint, nil, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		fmt.Println("Guided review deleted.")
		return nil
	},
	Flags: flags.AllDefaultFlags,
}
