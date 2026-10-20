package main

import (
	"fmt"
	"os"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/project"
	"github.com/go-slides/slides/theme"
)

// options are the command-line flags.
type options struct {
	output     string
	configFile string
	noConfig   bool
	themeSets  []string
	kindFiles  []string
	dataFiles  []string
	sets       []string
	html       bool
	lang       string
	bare       bool
	noTemplate bool
	strictYAML bool
	noEmoji    bool

	pdf         bool
	pdfRaster   bool // --pdf-raster: the PDF's pages as images
	pptxRaster  bool // --pptx-raster: one picture a slide
	odpRaster   bool // --odp-raster: one picture a slide
	pptx        bool
	odp         bool
	images      string
	image       string
	jpegQuality int

	imageScale      float64
	allowLocalFiles bool

	// themeOverride is --theme: a theme name or a CSS file.
	themeOverride string
	// meta are the metadata global directives of the command line.
	meta slides.Override
}

// project returns the flags that shape the build configuration.
func (o *options) project() project.Options {
	return project.Options{
		ConfigFile: o.configFile,
		NoConfig:   o.noConfig,
		ThemeSets:  o.themeSets,
		KindFiles:  o.kindFiles,
		DataFiles:  o.dataFiles,
		Sets:       o.sets,
		HTML:       o.html,
		Lang:       o.lang,
		Bare:       o.bare,
		NoTemplate: o.noTemplate,
		StrictYAML: o.strictYAML,
		NoEmoji:    o.noEmoji,
	}
}

// config assembles the build configuration: built-ins, then the
// configuration file, then the flags (see package project), then the
// command line's global directives.
func (o *options) config(deckPath string) (slides.Config, error) {
	cfg, err := project.Load(deckPath, o.project())
	if err != nil {
		return cfg, err
	}
	cfg.Override = o.meta
	if o.themeOverride != "" {
		name, err := overrideTheme(cfg.Themes, o.themeOverride)
		if err != nil {
			return cfg, err
		}
		cfg.Override.Theme = name
	}
	return cfg, nil
}

func (o *options) themeSet() (*theme.Set, error) {
	return project.ThemeSet(nil, o.themeSets...)
}

// overrideTheme resolves --theme as marp-cli does: a CSS file is added to
// the theme set under a name of its own, overriding any @theme it declares,
// and is the deck's theme; anything that is not a file is a theme name. A
// directory is an error: it is what --theme-set is for.
func overrideTheme(set *theme.Set, arg string) (string, error) {
	st, err := os.Stat(arg)
	if err != nil {
		if os.IsNotExist(err) {
			return arg, nil
		}
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("--theme %s: a directory cannot be the theme; use --theme-set to make its CSS files available", arg)
	}
	b, err := os.ReadFile(arg)
	if err != nil {
		return "", err
	}
	const name = "cli-theme-override"
	if _, err := set.Add(string(b) + "\n/* @theme " + name + " */"); err != nil {
		return "", fmt.Errorf("%s: %v", arg, err)
	}
	return name, nil
}
