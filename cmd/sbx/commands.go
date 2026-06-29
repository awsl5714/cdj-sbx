package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/awsl5714/cdj-sbx/internal/apply"
	"github.com/awsl5714/cdj-sbx/internal/config"
	"github.com/awsl5714/cdj-sbx/internal/link"
	"github.com/awsl5714/cdj-sbx/internal/model"
	"github.com/awsl5714/cdj-sbx/internal/output"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Adopt an existing config: validate and set up a git baseline",
		RunE: func(cmd *cobra.Command, args []string) error {
			o := buildOptions(false, false)
			if err := apply.Verify(o); err != nil {
				return err
			}
			if o.Git != nil && o.Git.Enabled {
				if err := o.Git.Commit("sbx: baseline", o.ConfigPath); err != nil {
					return &apply.Error{Kind: apply.KindIO, Detail: err.Error(), Err: err}
				}
			}
			if jsonOut {
				output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: "init"})
			} else {
				fmt.Println("ok: config validated, git baseline ready")
			}
			return nil
		},
	}
}

func newUserCmd() *cobra.Command {
	c := &cobra.Command{Use: "user", Short: "Manage users"}
	c.AddCommand(newUserAddCmd(), newUserDelCmd(), newUserListCmd())
	return c
}

func newUserAddCmd() *cobra.Command {
	var uuidFlag string
	var dryRun, noReload bool
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a user to both inbounds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &output.UsageError{Msg: "usage: sbx user add <name>"}
			}
			secret := uuidFlag
			if secret == "" {
				secret = uuid.NewString()
			}
			res, err := apply.AddUser(buildOptions(dryRun, noReload), args[0], secret)
			if err != nil {
				return err
			}
			emitMutation("user.add", res)
			return nil
		},
	}
	cmd.Flags().StringVar(&uuidFlag, "uuid", "", "use this uuid instead of generating one")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and show the diff without writing")
	cmd.Flags().BoolVar(&noReload, "no-reload", false, "skip reloading sing-box")
	return cmd
}

func newUserDelCmd() *cobra.Command {
	var dryRun, noReload bool
	cmd := &cobra.Command{
		Use:   "del <name>",
		Short: "Remove a user from both inbounds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &output.UsageError{Msg: "usage: sbx user del <name>"}
			}
			res, err := apply.DelUser(buildOptions(dryRun, noReload), args[0])
			if err != nil {
				return err
			}
			emitMutation("user.del", res)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and show the diff without writing")
	cmd.Flags().BoolVar(&noReload, "no-reload", false, "skip reloading sing-box")
	return cmd
}

func newUserListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List managed users",
		RunE: func(cmd *cobra.Command, args []string) error {
			users, err := apply.ListUsers(buildOptions(false, false))
			if err != nil {
				return err
			}
			if jsonOut {
				output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: "user.list", Result: users})
				return nil
			}
			for _, u := range users {
				fmt.Println(u.Name)
			}
			return nil
		},
	}
}

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check schema (sing-box) and invariants",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := apply.Verify(buildOptions(false, false)); err != nil {
				return err
			}
			if jsonOut {
				output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: "verify"})
			} else {
				fmt.Println("ok: schema + invariants pass")
			}
			return nil
		},
	}
}

func newReloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Validate then reload sing-box",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := apply.Reload(buildOptions(false, false)); err != nil {
				return err
			}
			if jsonOut {
				output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: "reload"})
			} else {
				fmt.Println("ok: reloaded")
			}
			return nil
		},
	}
}

func newLinkCmd() *cobra.Command {
	var server, format string
	cmd := &cobra.Command{
		Use:   "link <name>",
		Short: "Print vless:// / hysteria2:// share links for a user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &output.UsageError{Msg: "usage: sbx link <name>"}
			}
			if server == "" {
				return &output.UsageError{Msg: "provide the public server address with --server <host>"}
			}
			name := args[0]
			c, err := config.Load(cfgPath)
			if err != nil {
				return &apply.Error{Kind: apply.KindIO, Detail: err.Error(), Err: err}
			}
			users, err := c.Users("reality-in", "uuid")
			if err != nil {
				return &apply.Error{Kind: apply.KindInboundNotFound, Detail: err.Error(), Err: err}
			}
			secret := ""
			for _, u := range users {
				if u.Name == name {
					secret = u.Secret
					break
				}
			}
			if secret == "" {
				return &apply.Error{Kind: apply.KindUserNotFound, Detail: "user not found: " + name}
			}
			rp, _ := c.RealityParams("reality-in")
			hp, _ := c.Hy2Params("hy2-in")
			u := model.User{Name: name, Secret: secret}
			vlessLink, err := link.VLESS(name, server, u, rp)
			if err != nil {
				return &apply.Error{Kind: apply.KindIO, Detail: err.Error(), Err: err}
			}
			hy2Link := link.Hysteria2(name, server, u, hp)

			var out []string
			switch format {
			case "vless":
				out = []string{vlessLink}
			case "hy2":
				out = []string{hy2Link}
			case "sub":
				out = []string{link.Subscription([]string{vlessLink, hy2Link})}
			case "all", "":
				out = []string{vlessLink, hy2Link}
			default:
				return &output.UsageError{Msg: "unknown --format (vless|hy2|sub|all)"}
			}
			if jsonOut {
				output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: "link", Result: out})
			} else {
				fmt.Println(strings.Join(out, "\n"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "public server address (host or IP) for the links")
	cmd.Flags().StringVar(&format, "format", "all", "vless|hy2|sub|all")
	return cmd
}

func emitMutation(action string, res *apply.Result) {
	if jsonOut {
		output.EmitJSON(os.Stdout, output.Envelope{OK: true, Action: action, DryRun: res.DryRun, Result: res})
		return
	}
	if res.DryRun {
		fmt.Println("[dry-run] no changes written; diff:")
		fmt.Print(res.Diff)
		return
	}
	fmt.Printf("ok: %s %s", action, res.User)
	if res.UUID != "" {
		fmt.Printf(" (uuid %s)", res.UUID)
	}
	fmt.Println()
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
}
