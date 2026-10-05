package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"komrad/internal/principalrole"
	"komrad/internal/principals"
	"komrad/internal/store"
	identitystore "komrad/internal/store/identity"
	"komrad/lib/xslice"

	"github.com/spf13/cobra"
)

const issuedTokensNote = "tokens already issued keep working until they expire"

type principalEditor interface {
	Add(ctx context.Context, params principals.AddParams) (*identitystore.Principal, error)
	List(ctx context.Context) ([]principals.Listed, error)
	Bind(ctx context.Context, params principals.BindParams) (principalrole.Role, error)
	Unbind(ctx context.Context, principalName, engineName string) error
	Disable(ctx context.Context, name string) (*identitystore.Principal, error)
	Enable(ctx context.Context, name string) (*identitystore.Principal, error)
	Remove(ctx context.Context, name string) (int64, error)
}

type principalOpener func(ctx context.Context) (principalEditor, func(), error)

func principalCmd(state *app) *cobra.Command {
	return newPrincipalCmd(func(ctx context.Context) (principalEditor, func(), error) {
		pool, err := store.OpenOwner(ctx, state.env.OwnerDatabaseURL)
		if err != nil {
			return nil, nil, err
		}

		service := principals.NewPrincipalService(principals.NewPrincipalServiceParams{
			Principals: identitystore.NewPrincipalStore(pool),
			Bindings:   identitystore.NewBindingStore(pool),
		})

		return service, pool.Close, nil
	})
}

func newPrincipalCmd(open principalOpener) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "principal",
		Short: "Manage the people and services that may call an engine's api",
		Long: `Manage the people and services that may call an engine's api.

A principal is a person or a service. A binding gives it one role on one engine:
viewer, operator or automation. These records sit beside the engines rather than
inside them, and no engine can read them. Every command here connects as
owner_database_url, the login komrad migrate runs as.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(
		principalAddCmd(open),
		principalListCmd(open),
		principalBindCmd(open),
		principalUnbindCmd(open),
		principalDisableCmd(open),
		principalEnableCmd(open),
		principalRemoveCmd(open),
	)

	return cmd
}

func principalAddCmd(open principalOpener) *cobra.Command {
	var provider, subject string
	var service bool

	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add a person, or a service with --service",
		Long: `Add a person, or a service with --service.

A person names the outside provider that vouches for them and the provider's
stable id for them: the sub claim for Cloudflare Access and Google, and the numeric
user id for GitHub, never a login or an email. A service principal may name one
too, or neither. It prints the principal's id, which is the subject of every token
issued to it.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := identitystore.KindPerson
			if service {
				kind = identitystore.KindService
			}

			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				added, err := editor.Add(cmd.Context(), principals.AddParams{
					Name:     args[0],
					Kind:     kind,
					Provider: provider,
					Subject:  subject,
				})
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "added %s, id %s", added.Name, added.ID)
			})
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "the provider that vouches for the principal: cloudflare_access, github or google")
	cmd.Flags().StringVar(&subject, "subject", "", "the provider's stable id for the principal")
	cmd.Flags().BoolVar(&service, "service", false, "add a service principal rather than a person")

	return cmd
}

func principalListCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "List every principal with its bindings",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				listed, err := editor.List(cmd.Context())
				if err != nil {
					return err
				}

				return printTable(cmd.OutOrStdout(),
					[]string{"NAME", "ID", "KIND", "IDENTITY", "STATE", "BINDINGS"},
					xslice.Map(listed, describePrincipal))
			})
		},
	}
}

func principalBindCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:   "bind NAME ENGINE ROLE",
		Short: "Give a principal a role on an engine, replacing any it had",
		Long: `Give a principal a role on an engine, replacing any it had.

The role is viewer, operator or automation. The engine is named as komrad engine
ensure named it, and is an argument rather than --engine, which picks the config.`,
		Args:          cobra.ExactArgs(3),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				previous, err := editor.Bind(cmd.Context(), principals.BindParams{
					Principal: args[0],
					Engine:    args[1],
					Role:      args[2],
				})
				if err != nil {
					return err
				}

				if previous == "" {
					return printLine(cmd.OutOrStdout(), "%s is %s on %s", args[0], args[2], args[1])
				}

				return printLine(cmd.OutOrStdout(), "%s is %s on %s, was %s", args[0], args[2], args[1], previous)
			})
		},
	}
}

func principalUnbindCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:           "unbind NAME ENGINE",
		Short:         "Take away a principal's role on an engine",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				if err := editor.Unbind(cmd.Context(), args[0], args[1]); err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "%s has no role on %s", args[0], args[1])
			})
		},
	}
}

func principalDisableCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:           "disable NAME",
		Short:         "Stop a principal being issued tokens, keeping its bindings",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				principal, err := editor.Disable(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "disabled %s; %s", principal.Name, issuedTokensNote)
			})
		},
	}
}

func principalEnableCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:           "enable NAME",
		Short:         "Let a disabled principal be issued tokens again, with the bindings it had",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				principal, err := editor.Enable(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "enabled %s", principal.Name)
			})
		},
	}
}

func principalRemoveCmd(open principalOpener) *cobra.Command {
	return &cobra.Command{
		Use:           "remove NAME",
		Short:         "Delete a principal and its bindings",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPrincipals(cmd, open, func(editor principalEditor) error {
				bindings, err := editor.Remove(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "removed %s and %d %s; %s", args[0], bindings, pluralize(bindings, "binding"), issuedTokensNote)
			})
		},
	}
}

func runWithPrincipals(cmd *cobra.Command, open principalOpener, run func(principalEditor) error) error {
	editor, closeEditor, err := open(cmd.Context())
	if err != nil {
		return err
	}
	defer closeEditor()

	return run(editor)
}

func describePrincipal(listed principals.Listed) []string {
	principal := listed.Principal

	identity := "-"
	if principal.Provider != nil && principal.Subject != nil {
		identity = string(*principal.Provider) + ":" + *principal.Subject
	}

	state := "active"
	if principal.DisabledAt != nil {
		state = "disabled"
	}

	bindings := "-"
	if len(listed.Bindings) > 0 {
		bindings = strings.Join(xslice.Map(listed.Bindings, func(binding identitystore.Binding) string {
			return binding.EngineName + ":" + string(binding.Role)
		}), ",")
	}

	return []string{principal.Name, principal.ID, string(principal.Kind), identity, state, bindings}
}

func printLine(out io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(out, format+"\n", args...); err != nil {
		return fmt.Errorf("failed to print: %w", err)
	}

	return nil
}

func pluralize(count int64, noun string) string {
	if count == 1 {
		return noun
	}

	return noun + "s"
}
