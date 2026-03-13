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

type diffReviewOrderItem struct {
	File      string `json:"file"`
	HunkIndex *int   `json:"hunk_index,omitempty"`
	Label     string `json:"label,omitempty"`
}

type diffReviewOrderRequest struct {
	Items []*diffReviewOrderItem `json:"items"`
}

type diffReviewOrderResponse struct {
	Items     []*diffReviewOrderItem `json:"items"`
	UpdatedAt string                 `json:"updated_at"`
}

// CmdPullsDiffOrder manages diff review order for a pull request
var CmdPullsDiffOrder = cli.Command{
	Name:        "diff-order",
	Usage:       "Manage the diff review order for a pull request",
	Description: "Get, set, or delete the custom review order that controls how files and hunks are displayed in the diff view",
	Commands: []*cli.Command{
		&cmdDiffOrderGet,
		&cmdDiffOrderSet,
		&cmdDiffOrderDelete,
		&cmdDiffOrderListHunks,
	},
}

var cmdDiffOrderGet = cli.Command{
	Name:      "get",
	Usage:     "Get the diff review order for a pull request",
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
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/diff-order", ctx.Owner, ctx.Repo, idx)
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

		var order diffReviewOrderResponse
		if err := json.Unmarshal(body, &order); err != nil {
			return err
		}

		if len(order.Items) == 0 {
			fmt.Println("No custom review order set for this pull request.")
			return nil
		}

		fmt.Printf("Review order (updated %s):\n\n", order.UpdatedAt)
		for i, item := range order.Items {
			prefix := fmt.Sprintf("  %d. ", i+1)
			if item.HunkIndex != nil {
				fmt.Printf("%s%s (hunk %d)", prefix, item.File, *item.HunkIndex)
			} else {
				fmt.Printf("%s%s", prefix, item.File)
			}
			if item.Label != "" {
				// Show first line of label inline
				firstLine := strings.SplitN(item.Label, "\n", 2)[0]
				fmt.Printf("  — %s", firstLine)
			}
			fmt.Println()
		}
		return nil
	},
	Flags: flags.AllDefaultFlags,
}

var cmdDiffOrderSet = cli.Command{
	Name:  "set",
	Usage: "Set the diff review order for a pull request",
	Description: `Set a custom order for files and hunks in a PR diff view.

Specify files in the order you want reviewers to see them.
Use --file/-f to add files in order, with optional labels after a colon.
Use --hunk/-H to specify hunk-level ordering as file:index or file:index:label.

Examples:
  tea pulls diff-order set 1 -f "pkg/types.go:Start here" -f pkg/handler.go -f tests/handler_test.go
  tea pulls diff-order set 1 -f pkg/handler.go -H "pkg/handler.go:2:Core change" -H "pkg/handler.go:0:Imports"
  tea pulls diff-order set 1 --from-json order.json
  cat order.json | tea pulls diff-order set 1 --from-json -`,
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

		var items []*diffReviewOrderItem

		// If --from-json is provided, read items from a JSON file or stdin
		jsonFile := cmd.String("from-json")
		if jsonFile != "" {
			var data []byte
			var err error
			if jsonFile == "-" {
				data, err = io.ReadAll(os.Stdin)
			} else {
				data, err = os.ReadFile(jsonFile)
			}
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", jsonFile, err)
			}
			var req diffReviewOrderRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("failed to parse JSON: %w", err)
			}
			items = req.Items
		} else {
			// Build items from --file and --hunk flags
			for _, f := range cmd.StringSlice("file") {
				parts := strings.SplitN(f, ":", 2)
				item := &diffReviewOrderItem{File: parts[0]}
				if len(parts) == 2 {
					item.Label = parts[1]
				}
				items = append(items, item)
			}

			for _, h := range cmd.StringSlice("hunk") {
				parts := strings.SplitN(h, ":", 3)
				if len(parts) < 2 {
					return fmt.Errorf("invalid hunk format %q, expected file:index or file:index:label", h)
				}
				hunkIdx, err := utils.ArgToIndex(parts[1])
				if err != nil {
					return fmt.Errorf("invalid hunk index in %q: %w", h, err)
				}
				idx := int(hunkIdx)
				item := &diffReviewOrderItem{
					File:      parts[0],
					HunkIndex: &idx,
				}
				if len(parts) == 3 {
					item.Label = parts[2]
				}
				items = append(items, item)
			}
		}

		if len(items) == 0 {
			return fmt.Errorf("no items specified. Use --file, --hunk, or --from-json")
		}

		reqBody := diffReviewOrderRequest{Items: items}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}

		client := api.NewClient(ctx.Login)
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/diff-order", ctx.Owner, ctx.Repo, idx)
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

		fmt.Printf("Review order set with %d items.\n", len(items))
		return nil
	},
	Flags: append([]cli.Flag{
		&cli.StringSliceFlag{
			Name:    "file",
			Aliases: []string{"f"},
			Usage:   "Add a file in order (format: path or path:label)",
		},
		&cli.StringSliceFlag{
			Name:    "hunk",
			Aliases: []string{"H"},
			Usage:   "Add a hunk in order (format: path:index or path:index:label)",
		},
		&cli.StringFlag{
			Name:  "from-json",
			Usage: "Read order items from a JSON file (use - for stdin)",
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

var cmdDiffOrderListHunks = cli.Command{
	Name:      "list-hunks",
	Aliases:   []string{"hunks"},
	Usage:     "List hunks for files in a pull request diff",
	ArgsUsage: "<pull index> [file]",
	Description: `Shows the hunks (sections) for each file in a PR diff with their index,
line range, and change count. Use this to plan hunk-level reordering.

If a file path is given, only show hunks for that file.

Examples:
  tea pulls diff-order list-hunks 2
  tea pulls diff-order list-hunks 2 big_file.go`,
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
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/diff-order/hunks", ctx.Owner, ctx.Repo, idx)
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

var cmdDiffOrderDelete = cli.Command{
	Name:      "delete",
	Aliases:   []string{"rm"},
	Usage:     "Delete the diff review order for a pull request",
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
		endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%d/diff-order", ctx.Owner, ctx.Repo, idx)
		resp, err := client.Do("DELETE", endpoint, nil, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 204 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
		}

		fmt.Println("Review order deleted.")
		return nil
	},
	Flags: flags.AllDefaultFlags,
}
