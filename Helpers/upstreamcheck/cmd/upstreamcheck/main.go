package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/bootstrap"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/check"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/config"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/gitx"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/manifest"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/migrate"
	"github.com/cookiengineer/retrokit/Helpers/upstreamcheck/internal/vendor"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "upstreamcheck:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "init":
		return cmdInit(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "lint":
		return cmdLint(args[1:])
	case "plan":
		return cmdPlan(args[1:])
	case "refs":
		return cmdRefs(args[1:])
	case "leaks":
		return cmdLeaks(args[1:])
	case "equivalence":
		return cmdEquivalence(args[1:])
	case "report":
		return cmdReport(args[1:])
	case "migrate":
		return cmdMigrate(args[1:])
	case "vendor":
		return cmdVendor(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Print(`upstreamcheck - maintain and verify the RetroKit removal layer

Usage:
  upstreamcheck init    [--config FILE] [--base REF] [--fork REF]
  upstreamcheck status  [--config FILE] [--base REF] [--fork REF] [--target REF] [--json]
  upstreamcheck lint    [--config FILE] [--base REF] [--fork REF]
  upstreamcheck plan    [--config FILE] [--target REF] [--feature ID] [--json]
  upstreamcheck refs    [--config FILE] [--fork REF] [--feature ID] [--limit N]
  upstreamcheck leaks   [--config FILE] [--fork REF] [--feature ID] [--limit N]
  upstreamcheck equivalence [--config FILE] [--fork REF]
  upstreamcheck report  [--config FILE] [--base REF] [--target REF] [--out FILE]
  upstreamcheck migrate --feature ID [--base REF] [--target REF] --out DIR
  upstreamcheck vendor import --upstream DIR [--version VERSION] [--base-tag TAG] [--base-from REF] [--tag TAG]
  upstreamcheck version

The manifest and configuration live in Helpers/upstreamcheck.
`)
}

type commonFlags struct {
	config   string
	base     string
	fork     string
	target   string
	json     bool
	feature  string
	limit    int
	out      string
	strict   bool
	upstream string
	version  string
	baseTag  string
	baseFrom string
	tag      string
}

func parseCommon(name string, args []string) (*commonFlags, *flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	cf := &commonFlags{}
	fs.StringVar(&cf.config, "config", "", "path to upstreamcheck.toml")
	fs.StringVar(&cf.base, "base", "", "baseline ref")
	fs.StringVar(&cf.fork, "fork", "HEAD", "fork ref")
	fs.StringVar(&cf.target, "target", "", "target vendor ref")
	fs.BoolVar(&cf.json, "json", false, "emit JSON")
	fs.StringVar(&cf.feature, "feature", "", "feature id")
	fs.IntVar(&cf.limit, "limit", 200, "maximum lines for refs")
	fs.StringVar(&cf.out, "out", "", "output directory")
	fs.BoolVar(&cf.strict, "strict", false, "treat warnings as errors")
	fs.StringVar(&cf.upstream, "upstream", "", "path to upstream WebKit git checkout")
	fs.StringVar(&cf.version, "version", "", "upstream version")
	fs.StringVar(&cf.baseTag, "base-tag", "", "vendor baseline tag")
	fs.StringVar(&cf.baseFrom, "base-from", "", "ref to build the vendor baseline from")
	fs.StringVar(&cf.tag, "tag", "", "vendor target tag")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return cf, fs, nil
}

func open(args []string) (*gitx.Repo, *config.Config, *commonFlags, error) {
	cf, _, err := parseCommon("upstreamcheck", args)
	if err != nil {
		return nil, nil, nil, err
	}
	root, err := config.FindRepoRoot()
	if err != nil {
		return nil, nil, nil, err
	}
	configPath := cf.config
	if configPath == "" {
		configPath = filepath.Join(root, "Helpers", "upstreamcheck", "upstreamcheck.toml")
	}
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, err
	}
	repo, err := gitx.Open(root)
	if err != nil {
		return nil, nil, nil, err
	}
	return repo, cfg, cf, nil
}

func refOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// defaultBase is the fork's true divergence base. The vendor base is a
// filtered tree and is intentionally missing Tools/Documentation, so using it
// for the fork summary would report those directories as new files.
func defaultBase(repo *gitx.Repo, cfg *config.Config, override string) string {
	if override != "" {
		return override
	}
	if cfg.Vendor.BootstrapBase != "" && repo.RefExists(cfg.Vendor.BootstrapBase) {
		return cfg.Vendor.BootstrapBase
	}
	if root, err := repo.RootCommit("legacy/2.34"); err == nil {
		return root
	}
	return cfg.Vendor.BaseRef
}

func cmdInit(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	base := defaultBase(repo, cfg, cf.base)
	if base == "" {
		return fmt.Errorf("could not determine baseline ref; pass --base")
	}
	fork := refOr(cf.fork, "HEAD")
	if !repo.RefExists(base) {
		return fmt.Errorf("baseline ref %s does not exist", base)
	}
	result, err := bootstrap.Run(repo, cfg, base, fork)
	if err != nil {
		return err
	}
	if err := manifest.WriteEntries(cfg.ManifestPath(cfg.Manifest.Divergence), result.Entries); err != nil {
		return err
	}
	if err := manifest.WriteEntries(cfg.ManifestPath(cfg.Manifest.OutOfScope), result.OutOfScope); err != nil {
		return err
	}
	if err := manifest.WriteFeatures(cfg.ManifestPath(cfg.Manifest.Features), result.Features); err != nil {
		return err
	}
	if err := writeReview(cfg.ManifestPath(cfg.Manifest.Review), result.Review); err != nil {
		return err
	}

	inScope := 0
	for _, entry := range result.Entries {
		if entry.Kind == "file" {
			inScope++
		}
	}
	fmt.Printf("baseline: %s\nfork: %s\ncommits: %d\n", base, fork, result.CommitCount)
	fmt.Printf("in-scope file entries: %d\nout-of-scope file entries: %d\nmanifest rows: %d\nfeatures: %d\nreview items: %d\n",
		inScope, countFiles(result.OutOfScope), len(result.Entries), len(result.Features), len(result.Review))
	fmt.Printf("wrote:\n  %s\n  %s\n  %s\n  %s\n",
		cfg.ManifestPath(cfg.Manifest.Divergence),
		cfg.ManifestPath(cfg.Manifest.OutOfScope),
		cfg.ManifestPath(cfg.Manifest.Features),
		cfg.ManifestPath(cfg.Manifest.Review))
	return nil
}

func countFiles(entries []manifest.Entry) int {
	n := 0
	for _, entry := range entries {
		if entry.Kind == "file" {
			n++
		}
	}
	return n
}

func writeReview(path string, items []bootstrap.ReviewItem) error {
	var b strings.Builder
	b.WriteString("# commit\tsubject\tpaths\n")
	for _, item := range items {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", item.Commit, strings.ReplaceAll(item.Subject, "\t", " "), strings.Join(item.Paths, " "))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func cmdStatus(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	base := defaultBase(repo, cfg, cf.base)
	fork := refOr(cf.fork, "HEAD")
	target := refOr(cf.target, cfg.Vendor.TargetRef)
	if !repo.RefExists(base) {
		return fmt.Errorf("baseline ref %s does not exist; run `upstreamcheck init --base fbd7f1a9` first", base)
	}
	result, err := check.Status(repo, cfg, base, fork, target)
	if err != nil {
		return err
	}
	if cf.json {
		return printJSON(result)
	}
	fmt.Printf("baseline: %s\nfork: %s\ntarget: %s\n\n", result.BaseRef, result.ForkRef, result.TargetRef)
	fmt.Printf("changed paths: %d (in scope: %d, out of scope: %d)\n", result.ChangedPaths, result.InScopePaths, result.OutOfScope)
	fmt.Printf("manifest rows: %d\nfeatures: %d\nunmapped paths: %d\n", result.ManifestRows, result.Features, len(result.Unmapped))
	if result.TargetPresent > 0 {
		fmt.Printf("target work items still present: %d\n", result.TargetPresent)
	}
	if len(result.Unmapped) > 0 {
		fmt.Println("\nunmapped paths:")
		for _, path := range result.Unmapped {
			fmt.Println("  " + path)
		}
	}
	fmt.Println("\nentries by feature:")
	for _, feature := range sortedKeys(result.Counts) {
		counts := result.Counts[feature]
		fmt.Printf("  %-28s removed=%d modified=%d added=%d\n", feature,
			counts["removed"], counts["modified"], counts["added"])
	}
	return nil
}

func cmdLint(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	base := defaultBase(repo, cfg, cf.base)
	fork := refOr(cf.fork, "HEAD")
	if !repo.RefExists(base) {
		return fmt.Errorf("baseline ref %s does not exist", base)
	}
	result, err := check.Lint(repo, cfg, base, fork)
	if err != nil {
		return err
	}
	for _, issue := range result.Errors {
		fmt.Printf("ERROR [%s] %s\n", issue.Kind, issue.Message)
	}
	for _, issue := range result.Warnings {
		fmt.Printf("WARN  [%s] %s\n", issue.Kind, issue.Message)
	}
	fmt.Printf("\nchecked %d changed paths, %d errors, %d warnings\n", result.Checked, len(result.Errors), len(result.Warnings))
	if len(result.Errors) > 0 || cf.strict && len(result.Warnings) > 0 {
		os.Exit(1)
	}
	return nil
}

func cmdPlan(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	base := defaultBase(repo, cfg, cf.base)
	target := refOr(cf.target, cfg.Vendor.TargetRef)
	result, err := check.Plan(repo, cfg, base, target)
	if err != nil {
		return err
	}
	if cf.json {
		return printJSON(result)
	}
	fmt.Printf("target: %s\n\n", result.TargetRef)
	for _, feature := range result.Features {
		if cf.feature != "" && feature.Feature != cf.feature {
			continue
		}
		fmt.Printf("%s (%s): pending=%d done=%d manual=%d\n", feature.Feature, feature.Title, feature.Pending, feature.Done, feature.Manual)
		if cf.feature != "" {
			for _, plan := range feature.Entries {
				fmt.Printf("  %-8s %-12s %-8s %s", plan.Status, plan.Entry.Kind, plan.Entry.Disposition, plan.Entry.Path)
				if plan.Entry.Symbol != "-" {
					fmt.Printf(" :: %s", plan.Entry.Symbol)
				}
				if plan.Detail != "" {
					fmt.Printf("  (%s)", plan.Detail)
				}
				fmt.Println()
			}
		}
	}
	return nil
}

func cmdRefs(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	fork := refOr(cf.fork, "HEAD")
	lines, err := check.Refs(repo, cfg, fork, cf.feature, cf.limit)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	fmt.Printf("\n%d reference(s)\n", len(lines))
	return nil
}

func cmdLeaks(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	fork := refOr(cf.fork, "HEAD")
	result, err := check.Leaks(repo, cfg, fork, cf.feature, cf.limit)
	if err != nil {
		return err
	}
	if cf.json {
		return printJSON(result)
	}
	fmt.Printf("ref: %s\ntotal residual token occurrences: %d\n\n", result.Ref, result.Total)
	for _, feature := range result.Features {
		fmt.Printf("%s (%s): %d\n", feature.Feature, feature.Title, feature.Count)
		for _, sample := range feature.Samples {
			fmt.Printf("  %s:%d [%s] %s\n", sample.Path, sample.Line, sample.Token, sample.Content)
		}
	}
	return nil
}

func cmdEquivalence(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	fork := refOr(cf.fork, "HEAD")
	result, err := check.Equivalence(repo, cfg, fork)
	if err != nil {
		return err
	}
	for _, issue := range result.Errors {
		fmt.Printf("ERROR [%s] %s\n", issue.Kind, issue.Message)
	}
	for _, issue := range result.Warnings {
		fmt.Printf("WARN  [%s] %s\n", issue.Kind, issue.Message)
	}
	fmt.Printf("\nchecked %d removal entries against %s: %d errors, %d warnings\n",
		result.Checked, result.Ref, len(result.Errors), len(result.Warnings))
	if len(result.Errors) > 0 || cf.strict && len(result.Warnings) > 0 {
		os.Exit(1)
	}
	return nil
}

func cmdReport(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	base := defaultBase(repo, cfg, cf.base)
	target := refOr(cf.target, cfg.Vendor.TargetRef)
	plan, err := check.Plan(repo, cfg, base, target)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# RetroKit migration plan\n\n")
	fmt.Fprintf(&b, "- baseline: `%s`\n- fork: `%s`\n- target: `%s`\n\n", base, cf.fork, target)
	fmt.Fprintf(&b, "| feature | pending | done | manual |\n|---|---:|---:|---:|\n")
	for _, feature := range plan.Features {
		if cf.feature != "" && feature.Feature != cf.feature {
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d |\n", feature.Feature, feature.Pending, feature.Done, feature.Manual)
	}
	b.WriteString("\n")
	for _, feature := range plan.Features {
		if cf.feature != "" && feature.Feature != cf.feature {
			continue
		}
		if feature.Pending == 0 && feature.Manual == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%s)\n\n", feature.Feature, feature.Title)
		for _, entry := range feature.Entries {
			if entry.Status == "done" {
				continue
			}
			fmt.Fprintf(&b, "- **%s** `%s` `%s`", entry.Status, entry.Entry.Kind, entry.Entry.Path)
			if entry.Entry.Symbol != "-" {
				fmt.Fprintf(&b, " :: `%s`", entry.Entry.Symbol)
			}
			if entry.Detail != "" {
				fmt.Fprintf(&b, " — %s", entry.Detail)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	if cf.out != "" {
		if err := os.MkdirAll(filepath.Dir(cf.out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(cf.out, []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", cf.out)
		return nil
	}
	fmt.Print(b.String())
	return nil
}

func cmdMigrate(args []string) error {
	repo, cfg, cf, err := open(args)
	if err != nil {
		return err
	}
	if cf.feature == "" {
		return fmt.Errorf("--feature is required")
	}
	base := defaultBase(repo, cfg, cf.base)
	target := refOr(cf.target, cfg.Vendor.TargetRef)
	out := cf.out
	if out == "" {
		out = filepath.Join(cfg.Manifest.Dir, "candidates")
	}
	result, err := migrate.Generate(repo, cfg, base, target, cf.feature, out)
	if err != nil {
		return err
	}
	fmt.Printf("feature: %s\ntarget: %s\n\n", result.Feature, result.TargetRef)
	fmt.Printf("deleted files: %d\nmodified files: %d\nremoved symbols: %d\nremoved lines: %d\nmanual entries: %d\nmissing in target: %d\npossible new target files: %d\n",
		len(result.DeletedFiles), len(result.ModifiedFiles), result.RemovedSyms, result.RemovedLines,
		len(result.Manual), len(result.Missing), len(result.NewTargets))
	fmt.Printf("\npatch: %s\nreport: %s\n", result.PatchPath, result.ReportPath)
	return nil
}

func cmdVendor(args []string) error {
	if len(args) == 0 || args[0] != "import" {
		return fmt.Errorf("usage: upstreamcheck vendor import --upstream DIR [flags]")
	}
	repo, cfg, cf, err := open(args[1:])
	if err != nil {
		return err
	}
	upstream := cf.upstream
	if upstream == "" {
		upstream = cfg.Vendor.UpstreamDir
	}
	if upstream == "" {
		upstream = "../upstream_repo"
	}
	if !filepath.IsAbs(upstream) {
		upstream = filepath.Join(repo.Dir, upstream)
	}
	baseTag := cf.baseTag
	if baseTag == "" {
		baseTag = cfg.Vendor.BaseRef
	}
	targetTag := cf.tag
	if targetTag == "" {
		targetTag = cfg.Vendor.TargetRef
	}
	baseFrom := cf.baseFrom
	if baseFrom == "" {
		if root, err := repo.RootCommit("legacy/2.34"); err == nil {
			baseFrom = root
		} else if root, err := repo.RootCommit("main"); err == nil {
			baseFrom = root
		}
	}
	result, err := vendor.Import(repo, vendor.Options{
		UpstreamDir: upstream,
		Version:     cf.version,
		Paths:       cfg.Vendor.Paths,
		BaseTag:     baseTag,
		BaseFrom:    baseFrom,
		TargetTag:   targetTag,
	})
	if err != nil {
		return err
	}
	fmt.Printf("base tag:   %s -> %s\n", result.BaseTag, result.BaseCommit)
	fmt.Printf("target tag: %s -> %s\n", result.TargetTag, result.TargetCommit)
	fmt.Printf("upstream:   %s\n", result.UpstreamHead)
	fmt.Printf("corrected normalized blobs: %d\n", result.Corrected)
	return nil
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func sortedKeys(m map[string]map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
