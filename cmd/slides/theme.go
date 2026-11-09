package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/theme"
	"github.com/go-slides/slides/themepack"
)

// themeCmd is "slides theme": theme packages (*.gsta.xz).
func themeCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "theme",
		Short: "Pack, verify, install, list and remove theme packages (*.gsta.xz)",
		Long: `A theme package is one tar archive compressed with xz, <name>.gsta.xz
(Go-Slides Theme Archive), with theme.hcl at its
root (name, version, licence, extends, css, fonts, assets, kinds) and the files
it declares. It is untrusted input: pack and verify apply the same checks every
reader does (safe paths, size and ratio bounds, no link out of the package in
CSS or SVG, fonts that parse).

Installed packages live in the user's configuration directory (go-slides/themes)
and a deck's "theme: name" finds them. --theme x.gsta.xz uses a package
file directly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	var output string
	pack := &cobra.Command{
		Use:   "pack dir",
		Short: "Pack a theme directory (with a theme.hcl) into <name>.gsta.xz",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, b, err := themepack.Pack(args[0], themepack.Limits{})
			if err != nil {
				return err
			}
			if output == "" {
				output = meta.Name + themepack.Extension
			}
			if err := os.WriteFile(output, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s %s, %d bytes\n", output, meta.Name, meta.Version, len(b))
			return nil
		},
	}
	pack.Flags().StringVarP(&output, "output", "o", "", "output file (default: <name>.gsta.xz)")

	verify := &cobra.Command{
		Use:   "verify file.gsta.xz",
		Short: "Check a theme package: the archive, theme.hcl, every file, and go-slides' version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := themepack.Open(args[0], themepack.Limits{})
			if err != nil {
				return err
			}
			if err := p.CheckVersion(version()); err != nil {
				return err
			}
			m := p.Meta
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s ok: %d files, %d css, %d fonts, %d assets, %d kinds, license %s\n",
				m.Name, m.Version, len(p.Files), len(m.CSS), len(m.Fonts), len(m.Assets), len(m.Kinds), m.License)
			return nil
		},
	}

	install := &cobra.Command{
		Use:   "install file.gsta.xz",
		Short: "Check a theme package and install it for the current user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := themepack.UserStore()
			if err != nil {
				return err
			}
			p, err := themepack.Open(args[0], st.Limits)
			if err != nil {
				return err
			}
			if err := p.CheckVersion(version()); err != nil {
				return err
			}
			m, err := st.Install(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s %s in %s\n", m.Name, m.Version, st.Dir)
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List the installed theme packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := themepack.UserStore()
			if err != nil {
				return err
			}
			es, err := st.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, e := range es {
				if e.Err != nil {
					fmt.Fprintf(out, "%-16s ERROR %v\n", e.Name, e.Err)
					continue
				}
				fmt.Fprintf(out, "%-16s %-10s %-14s %s\n", e.Name, e.Meta.Version, e.Meta.License, e.Meta.Description)
			}
			return nil
		},
	}

	remove := &cobra.Command{
		Use:     "remove name",
		Aliases: []string{"rm"},
		Short:   "Remove an installed theme package",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := themepack.UserStore()
			if err != nil {
				return err
			}
			ok, err := st.Remove(args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("theme %q is not installed", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}

	root.AddCommand(pack, verify, install, list, remove)
	return root
}

// withInstalled lets a set find installed packages by name.
func withInstalled(set *theme.Set) {
	if set == nil {
		return
	}
	st, err := themepack.UserStore()
	if err != nil {
		return // no configuration directory: no installed packages
	}
	var ensure func(string) error
	ensure = func(name string) error {
		if err := set.Ensure(name); err != nil {
			return err
		}
		if !set.Has(name) {
			return diag.Errorf("theme.unknown", "the base theme %q is neither built in nor installed", name)
		}
		return nil
	}
	set.Installed = st.Resolver(version(), ensure)
}

// installedKinds are the kinds of the installed package called name; none
// when there is no such package.
func installedKinds(name string) (deck.Kinds, error) {
	st, err := themepack.UserStore()
	if err != nil {
		return nil, nil
	}
	p, ok, err := st.Open(name)
	if err != nil || !ok {
		return nil, err
	}
	return p.Kinds()
}

// isPackage reports whether arg names a theme package file.
func isPackage(arg string) bool {
	return themepack.IsPackage(arg)
}

// overridePackage adds the package file to the set under its own name and
// returns the kinds it brings.
func overridePackage(set *theme.Set, file string) (string, deck.Kinds, error) {
	p, err := themepack.Open(file, themepack.Limits{})
	if err != nil {
		return "", nil, err
	}
	if err := p.CheckVersion(version()); err != nil {
		return "", nil, fmt.Errorf("%s: %w", file, err)
	}
	if b := p.Meta.Extends; b != "" && !set.Has(b) {
		if err := set.Ensure(b); err != nil {
			return "", nil, err
		}
		if !set.Has(b) {
			return "", nil, diag.Errorf("theme.unknown", "%s extends %q, which is neither built in nor installed", file, b)
		}
	}
	css, err := p.CSS()
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", file, err)
	}
	if _, err := set.Add(css); err != nil {
		return "", nil, fmt.Errorf("%s: %w", file, err)
	}
	kinds, err := p.Kinds()
	if err != nil {
		return "", nil, err
	}
	return p.Meta.Name, kinds, nil
}
