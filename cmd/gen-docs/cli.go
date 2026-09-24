package main

import (
	"strings"

	"weave/lib/xslice"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandDoc struct {
	Slug      string    `json:"slug"`
	Path      string    `json:"path"`
	Parent    string    `json:"parent"`
	Usage     string    `json:"usage"`
	Aliases   []string  `json:"aliases"`
	Short     string    `json:"short"`
	Long      string    `json:"long"`
	Example   string    `json:"example"`
	Flags     []flagDoc `json:"flags"`
	Inherited []flagDoc `json:"inherited"`
	Children  []string  `json:"children"`
}

type flagDoc struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand"`
	Type      string `json:"type"`
	Default   string `json:"default"`
	Usage     string `json:"usage"`
}

// buildCLI lists every command but the rpc subcommands, which the API reference covers.
func buildCLI(root *cobra.Command) []commandDoc {
	var commands []commandDoc
	walkCommands(root, &commands)

	return commands
}

func walkCommands(cmd *cobra.Command, into *[]commandDoc) {
	doc := commandDoc{
		Slug:      commandSlug(cmd),
		Path:      cmd.CommandPath(),
		Usage:     cmd.UseLine(),
		Aliases:   append([]string{}, cmd.Aliases...),
		Short:     cmd.Short,
		Long:      strings.TrimSpace(cmd.Long),
		Example:   strings.TrimSpace(cmd.Example),
		Flags:     describeFlags(cmd.NonInheritedFlags()),
		Inherited: describeFlags(cmd.InheritedFlags()),
		Children:  []string{},
	}
	if cmd.HasParent() {
		doc.Parent = commandSlug(cmd.Parent())
	}

	var children []*cobra.Command
	if doc.Slug != "rpc" {
		for _, child := range cmd.Commands() {
			if child.Name() != "help" && child.Name() != "completion" {
				children = append(children, child)
			}
		}
	}

	doc.Children = append(doc.Children, xslice.Map(children, commandSlug)...)

	*into = append(*into, doc)

	for _, child := range children {
		walkCommands(child, into)
	}
}

func commandSlug(cmd *cobra.Command) string {
	return strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name())), " ", "/")
}

func describeFlags(set *pflag.FlagSet) []flagDoc {
	flags := []flagDoc{}
	set.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden || flag.Name == "help" {
			return
		}

		flags = append(flags, flagDoc{
			Name:      flag.Name,
			Shorthand: flag.Shorthand,
			Type:      flag.Value.Type(),
			Default:   flag.DefValue,
			Usage:     flag.Usage,
		})
	})

	return flags
}
