package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// baselineFile records, per adapter package, how many Repository methods are
// still hand-written. It sits next to the generator and may only go down.
const baselineFile = "handwritten_baseline.txt"

const baselineHeader = `# Hand-written Repository methods per adapter package.
# repo-adapter-gen lowers a count when it harvests a method and -check fails
# when a count rises. Raising a count by hand is a deliberate, reviewable
# decision that a new method cannot be generated.
`

// ratchet compares the hand-written counts of this run against the baseline.
// A count that rose fails in every mode. A count that fell is written back by
// a generating run and reported as stale by -check.
func ratchet(path string, counts map[string]int, check bool) error {
	base, err := readBaseline(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	adapters := make([]string, 0, len(counts))
	for a := range counts {
		adapters = append(adapters, a)
	}
	sort.Strings(adapters)
	var rose, fell []string
	for _, a := range adapters {
		b, ok := base[a]
		switch {
		case !ok:
			fell = append(fell, fmt.Sprintf("%s unrecorded, now %d", a, counts[a]))
		case counts[a] > b:
			rose = append(rose, fmt.Sprintf("%s %d -> %d", a, b, counts[a]))
		case counts[a] < b:
			fell = append(fell, fmt.Sprintf("%s %d -> %d", a, b, counts[a]))
		}
	}
	if len(rose) > 0 {
		return fmt.Errorf("hand-written Repository methods rose (%s); generate them, or raise %s deliberately if they cannot be", strings.Join(rose, ", "), path)
	}
	if len(fell) == 0 {
		return nil
	}
	if check {
		return fmt.Errorf("%s is stale (%s); run: go run ./tools/repo-adapter-gen", path, strings.Join(fell, ", "))
	}
	if err := writeBaseline(path, counts); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%s)\n", path, strings.Join(fell, ", "))
	return nil
}

// readBaseline parses "<adapter> <count>" lines, skipping blanks and comments.
func readBaseline(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, count, ok := strings.Cut(line, " ")
		n, err := strconv.Atoi(strings.TrimSpace(count))
		if !ok || err != nil {
			return nil, fmt.Errorf("%s: malformed line %q", path, line)
		}
		out[name] = n
	}
	return out, sc.Err()
}

func writeBaseline(path string, counts map[string]int) error {
	adapters := make([]string, 0, len(counts))
	for a := range counts {
		adapters = append(adapters, a)
	}
	sort.Strings(adapters)
	var b strings.Builder
	b.WriteString(baselineHeader)
	for _, a := range adapters {
		fmt.Fprintf(&b, "%s %d\n", a, counts[a])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
