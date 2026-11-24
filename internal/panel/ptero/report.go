package ptero

import (
	"fmt"
	"io"
	"strings"
)

// WriteReport prints the plan for an admin to read before applying it.
func WriteReport(out io.Writer, src *Source, p *Plan) error {
	var w strings.Builder
	report(&w, src, p)
	_, err := io.WriteString(out, w.String())
	return err
}

func report(w *strings.Builder, src *Source, p *Plan) {
	fmt.Fprintln(w, "Pterodactyl import: dry run (nothing has been written)")
	fmt.Fprintln(w)
	rows := []struct {
		what       string
		found, got int
	}{
		{"users", len(src.Users), len(p.Users)},
		{"nodes", len(src.Nodes), len(p.Nodes)},
		{"eggs", len(src.Eggs), len(p.Eggs)},
		{"servers", len(src.Servers), len(p.Servers)},
		{"subusers", len(src.Subusers), len(p.Subusers)},
		{"schedules", len(src.Schedules), len(p.Schedules)},
		{"database hosts", len(src.DBHosts), len(p.DBHosts)},
		{"databases", len(src.Databases), len(p.Databases)},
	}
	fmt.Fprintf(w, "  %-16s %8s %10s\n", "", "found", "imported")
	for _, r := range rows {
		mark := ""
		if r.got < r.found {
			mark = "  (see below)"
		}
		fmt.Fprintf(w, "  %-16s %8d %10d%s\n", r.what, r.found, r.got, mark)
	}
	ports := 0
	for _, s := range p.Servers {
		ports += 1 + len(s.Extra)
	}
	fmt.Fprintf(w, "  %-16s %8d %10d\n", "ports", len(src.Allocations), ports)
	fmt.Fprintln(w, "  (ports found counts every allocation, used or free; only ports servers use are imported)")

	sections := []struct {
		level Level
		title string
	}{
		{Blocking, "Must be fixed before --apply"},
		{Changed, "Imported differently: check these"},
		{Info, "Good to know"},
	}
	for _, s := range sections {
		var lines []string
		for _, n := range p.Notes {
			if n.Level != s.level {
				continue
			}
			if n.Subject != "" {
				lines = append(lines, n.Subject+": "+n.Msg)
			} else {
				lines = append(lines, n.Msg)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s (%d)\n", s.title, len(lines))
		for _, l := range lines {
			fmt.Fprintln(w, "  - "+wrap(l, 76, "    "))
		}
	}

	fmt.Fprintln(w)
	if p.Blocked() {
		fmt.Fprintln(w, "Result: cannot be applied yet. Fix the items above in Pterodactyl and run this again.")
		return
	}
	fmt.Fprintln(w, "Result: ready. Applying is not available in this version yet; this report shows what it will do.")
	if len(p.Nodes) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next steps, once applying is available:")
	fmt.Fprintln(w, "  1. Stop Wings on every node, so no server changes during the move.")
	fmt.Fprintln(w, "  2. Run this command again with --apply; it prints a token for each node.")
	fmt.Fprintln(w, "  3. On each node, install the GamePanel daemon and adopt the server files:")
	for _, n := range p.Nodes {
		fmt.Fprintf(w, "       %-20s files in %s\n", n.Name, n.WingsDir)
	}
	fmt.Fprintln(w, "  4. Start the daemons, check each server's console, then remove Wings.")
}

// wrap breaks s into lines of at most width runes; later lines get indent.
func wrap(s string, width int, indent string) string {
	var out strings.Builder
	line := 0
	for i, word := range strings.Fields(s) {
		if i > 0 {
			if line+1+len(word) > width {
				out.WriteString("\n" + indent)
				line = len(indent)
			} else {
				out.WriteByte(' ')
				line++
			}
		}
		out.WriteString(word)
		line += len(word)
	}
	return out.String()
}
