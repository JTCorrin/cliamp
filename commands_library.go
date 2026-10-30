package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	cli "github.com/urfave/cli/v3"

	"github.com/bjarneo/cliamp/config"
	"github.com/bjarneo/cliamp/external/library"
	"github.com/bjarneo/cliamp/ipc"
	"github.com/bjarneo/cliamp/provider"
)

// libraryCommand manages the music folders of the library. A running cliamp
// takes the change at once and scans. Without one, list, add and remove work
// on the folders in config.toml, and cliamp scans them when it starts.
func libraryCommand() *cli.Command {
	jsonFlag := &cli.BoolFlag{Name: "json", Usage: "machine-readable JSON output"}
	return &cli.Command{
		Name:  "library",
		Usage: "manage the music folders that the Library indexes",
		Description: "The Library indexes folders of music files, such as a mounted NAS share.\n" +
			"Add a folder and cliamp scans it in the background. list, add and remove\n" +
			"also work when cliamp is not running; scan needs a running cliamp.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list the folders",
				Flags: []cli.Flag{jsonFlag},
				Action: func(ctx context.Context, c *cli.Command) error {
					roots, err := libraryRoots("library.roots", "")
					if err != nil {
						return err
					}
					return printLibraryRoots(roots, c.Bool("json"))
				},
			},
			{
				Name:      "add",
				Usage:     "add a music folder and scan it",
				ArgsUsage: "</path/to/music>",
				Flags:     []cli.Flag{jsonFlag},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return fmt.Errorf("usage: cliamp library add /path/to/music")
					}
					roots, err := libraryRoots("library.add_root", c.Args().First())
					if err != nil {
						return err
					}
					return printLibraryRoots(roots, c.Bool("json"))
				},
			},
			{
				Name:      "remove",
				Usage:     "remove a music folder and the files indexed below it",
				ArgsUsage: "</path/to/music>",
				Flags:     []cli.Flag{jsonFlag},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return fmt.Errorf("usage: cliamp library remove /path/to/music")
					}
					roots, err := libraryRoots("library.remove_root", c.Args().First())
					if err != nil {
						return err
					}
					return printLibraryRoots(roots, c.Bool("json"))
				},
			},
			{
				Name:  "scan",
				Usage: "scan the folders for new, changed and removed files",
				Action: func(ctx context.Context, c *cli.Command) error {
					_, err := ipcSend("library.scan", ipc.Request{})
					return err
				},
			},
		},
	}
}

// libraryRoots runs op on the running cliamp, and on the config file when no
// cliamp runs and op can work there.
func libraryRoots(op, path string) ([]provider.LibraryRoot, error) {
	request := ipc.Request{}
	if path != "" {
		abs, err := library.AbsPath(path)
		if err != nil {
			return nil, err
		}
		request.Path = abs
	}
	response, err := ipcSend(op, request)
	if err == nil {
		roots := make([]provider.LibraryRoot, len(response.Roots))
		for i, r := range response.Roots {
			roots[i] = provider.LibraryRoot{Path: r.Path, Tracks: r.Tracks, Available: r.Available}
		}
		return roots, nil
	}
	if !errors.Is(err, ipc.ErrNotRunning) {
		return nil, err
	}
	return libraryRootsOffline(op, request.Path)
}

// libraryRootsOffline changes the folders in config.toml.
func libraryRootsOffline(op, path string) ([]provider.LibraryRoot, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	paths := slices.Clone(cfg.Library.Paths)
	switch op {
	case "library.add_root":
		root, err := library.CheckNewRoot(paths, path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, root)
		if err := config.SaveLibraryPaths(paths); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "cliamp is not running: the folder is saved and is scanned the next time cliamp starts.")
	case "library.remove_root":
		root := library.ExpandRoot(path)
		next := slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return library.ExpandRoot(p) == root })
		if len(next) == len(paths) {
			return nil, fmt.Errorf("library: %s is not in the library", root)
		}
		paths = next
		if err := config.SaveLibraryPaths(paths); err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "cliamp is not running: the folder is removed from the config.")
	case "library.roots":
	default:
		return nil, fmt.Errorf("%s needs a running cliamp", op)
	}
	roots := make([]provider.LibraryRoot, len(paths))
	for i, p := range paths {
		roots[i] = provider.LibraryRoot{Path: library.ExpandRoot(p), Available: true}
	}
	return roots, nil
}

// printLibraryRoots prints the folders, one for each line, or as JSON.
func printLibraryRoots(roots []provider.LibraryRoot, asJSON bool) error {
	if asJSON {
		items := make([]ipc.LibraryRootInfo, len(roots))
		for i, r := range roots {
			items[i] = ipc.LibraryRootInfo{Path: r.Path, Tracks: r.Tracks, Available: r.Available}
		}
		out, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	if len(roots) == 0 {
		fmt.Println("No music folders. Add one with: cliamp library add /path/to/music")
		return nil
	}
	for _, r := range roots {
		tracks := fmt.Sprintf("%d tracks", r.Tracks)
		if r.Tracks == 1 {
			tracks = "1 track"
		}
		switch {
		case !r.Available:
			fmt.Printf("%s  (not readable, showing the saved index: %s)\n", r.Path, tracks)
		case r.Tracks > 0:
			fmt.Printf("%s  (%s)\n", r.Path, tracks)
		default:
			fmt.Println(r.Path)
		}
	}
	return nil
}
