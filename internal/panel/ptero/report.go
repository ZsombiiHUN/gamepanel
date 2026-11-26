package ptero

import (
	"fmt"
	"io"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// WriteReport prints the plan for an admin to read before applying it.
func WriteReport(out io.Writer, src *Source, p *Plan, applying bool) error {
	var w strings.Builder
	report(&w, src, p, applying)
	_, err := io.WriteString(out, w.String())
	return err
}

func report(w *strings.Builder, src *Source, p *Plan, applying bool) {
	if applying {
		fmt.Fprintln(w, "Pterodactyl import")
	} else {
		fmt.Fprintln(w, "Pterodactyl import: dry run (nothing has been written)")
	}
	fmt.Fprintln(w)
	rows := []struct {
		what       string
		found, got int
	}{
		{"users", len(src.Users), len(p.Users)},
		{"locations", len(src.Locations), len(p.Locations)},
		{"nodes", len(src.Nodes), len(p.Nodes)},
		{"eggs", len(src.Eggs), len(p.Eggs)},
		{"servers", len(src.Servers), len(p.Servers)},
		{"subusers", len(src.Subusers), len(p.Subusers)},
		{"schedules", len(src.Schedules), len(p.Schedules)},
		{"database hosts", len(src.DBHosts), len(p.DBHosts)},
		{"databases", len(src.Databases), len(p.Databases)},
		{"API keys", len(src.APIKeys), len(p.APIKeys)},
		{"backups", len(src.Backups), len(p.Backups)},
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
	if !applying {
		fmt.Fprintln(w, "Result: ready. Run this again with --apply to import it into this panel.")
		return
	}
	fmt.Fprintln(w, "Result: ready. Importing...")
}

// WriteApplied prints what an applied import created and what to do on
// each node: the node's new token is shown here only.
func WriteApplied(out io.Writer, p *Plan, res store.ImportResult, dbPath, panelURL string) error {
	var b strings.Builder
	w := &b
	fmt.Fprintf(w, "\nImported into %s:\n", dbPath)
	fmt.Fprintf(w, "  %d account(s), %d location(s), %d node(s), %d egg(s), %d server(s), %d subuser(s), %d schedule(s), %d database host(s), %d database(s), %d API key(s), %d backup(s).\n",
		len(p.Users), len(p.Locations), len(p.Nodes), len(p.Eggs), len(p.Servers), len(p.Subusers), len(p.Schedules), len(p.DBHosts), len(p.Databases),
		len(p.APIKeys), len(p.Backups))
	panel := panelURL
	if panel == "" {
		panel = "<panel address>"
	}
	fmt.Fprintln(w, "\nNext, on each node (as root). Each token is shown only now; keep it until the node is set up.")
	for _, n := range p.Nodes {
		servers := 0
		for _, s := range p.Servers {
			if s.Server.NodeID == n.PteroID {
				servers++
			}
		}
		fmt.Fprintf(w, "\n  Node %q (%d server(s), Wings files in %s)\n", n.Name, servers, n.WingsDir)
		fmt.Fprintf(w, "    token: %s\n", res.NodeTokens[n.PteroID])
		fmt.Fprintln(w, "    1. systemctl disable --now wings")
		fmt.Fprintf(w, "    2. curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh \\\n")
		fmt.Fprintf(w, "         | bash -s -- node --panel %s --token <token> --adopt-wings %s\n", panel, n.WingsDir)
		if n.SFTPPort != 0 && n.SFTPPort != 2022 {
			fmt.Fprintf(w, "    3. Set GAMEPANEL_SFTP_ADDR=0.0.0.0:%d in /etc/gamepanel/daemon.env (SFTP keeps its port) and restart gamepanel-daemon\n", n.SFTPPort)
		}
	}
	fmt.Fprintln(w, "\nThe node's servers appear as soon as its daemon connects; ones Wings had running start again.")
	fmt.Fprintln(w, "Sign in with your Pterodactyl email and password.")
	_, err := io.WriteString(out, b.String())
	return err
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
