// Command sourcepos-study measures how well pandoc positions map back to the
// source for real files (task P0.2). It prints, per file, the number of words
// and spaces by status and the words that are not certified.
//
//	go run ./tools/sourcepos-study [-v] fichier.md…
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ptitan/glose/internal/sourcepos"
)

func main() {
	verbose := flag.Bool("v", false, "lister chaque mot non certifié")
	flag.Parse()
	total := map[sourcepos.Status]int{}
	for _, path := range flag.Args() {
		src, err := os.ReadFile(path)
		if err != nil {
			fail(err)
		}
		idx, err := sourcepos.NewIndex(src)
		if err != nil {
			fail(fmt.Errorf("%s : %w", path, err))
		}
		tree, err := sourcepos.Run(context.Background(), "pandoc", src)
		if err != nil {
			fail(err)
		}
		nodes, err := sourcepos.Nodes(tree)
		if err != nil {
			fail(err)
		}
		count := map[sourcepos.Status]int{}
		var odd []string
		for _, l := range sourcepos.Locate(idx, nodes) {
			if l.Kind != sourcepos.KindStr && l.Kind != sourcepos.KindSpace {
				continue
			}
			count[l.Status]++
			total[l.Status]++
			if l.Status != sourcepos.Certified && *verbose {
				src := ""
				if l.Status != sourcepos.Unknown {
					src = string(idx.Bytes()[l.Start:l.End])
				}
				odd = append(odd, fmt.Sprintf("    %-12s %v rendu %q source %q", l.Status, l.Ranges, l.Text, src))
			}
		}
		fmt.Printf("%s : %s\n", path, summary(count))
		if len(odd) > 0 {
			fmt.Println(strings.Join(odd, "\n"))
		}
	}
	if len(flag.Args()) > 1 {
		fmt.Printf("TOTAL : %s\n", summary(total))
	}
}

func summary(c map[sourcepos.Status]int) string {
	n := 0
	for _, v := range c {
		n += v
	}
	pct := 0.0
	if n > 0 {
		pct = 100 * float64(c[sourcepos.Certified]) / float64(n)
	}
	return fmt.Sprintf("%d mots et espaces : %d certifiés (%.2f %%), %d transformés, %d approximatifs, %d inconnus",
		n, c[sourcepos.Certified], pct, c[sourcepos.Transformed], c[sourcepos.Approximate], c[sourcepos.Unknown])
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "sourcepos-study :", err)
	os.Exit(1)
}
