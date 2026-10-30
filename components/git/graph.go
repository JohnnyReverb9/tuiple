package git

// Commit graph layout for the Log tab, in the spirit of the JetBrains log:
// one row per commit, each branch line in its own lane and colour, merges
// and forks drawn as connectors on the commit's own row.
//
// Commits must come children-first (`git log --date-order` or
// --topo-order). Each lane remembers the hash it is waiting for; a commit
// lands in the leftmost lane waiting for it, its first parent continues
// that lane, further parents open (or join) other lanes, and every other
// lane that was waiting for the same commit ends there. Until HEAD's row
// comes, lane 0 is held for it, so the checked-out branch runs down the
// left edge.

// graphCell is one lane column of a row: the lane glyph and the glyph of
// the spacer after it (a horizontal connector or a blank).
type graphCell struct {
	glyph       rune
	glyphColor  int // index into the lane palette; -1 = no colour
	spacer      rune
	spacerColor int
}

// graphRow is the drawing of one commit row.
type graphRow struct {
	cells []graphCell
	lane  int // lane of the commit dot
}

// laneState is a lane's colour and the commit it is waiting for ("" = free).
type laneState struct {
	want  string
	color int
}

const (
	glyphCommit = '●'
	glyphHead   = '◉'
)

// layoutGraph lays out commits (children first). head is the HEAD hash,
// drawn with a distinct dot.
func layoutGraph(commits []Commit, head string) []graphRow {
	var lanes []laneState
	nextColor := 0
	newColor := func() int {
		c := nextColor
		nextColor++
		return c
	}
	// freeLane finds a lane to reuse, never one in busy (lanes that end
	// or start on the current row, whose glyph is already taken).
	freeLane := func(busy map[int]bool) int {
		for i, l := range lanes {
			if l.want == "" && !busy[i] {
				return i
			}
		}
		lanes = append(lanes, laneState{})
		return len(lanes) - 1
	}

	// HEAD's line owns the leftmost lane, as in the JetBrains log, even
	// when other branches have newer commits.
	for _, c := range commits {
		if c.Hash == head {
			lanes = append(lanes, laneState{want: head, color: newColor()})
			break
		}
	}

	rows := make([]graphRow, 0, len(commits))
	for _, c := range commits {
		// Lanes waiting for this commit: the leftmost one carries it, the
		// rest are branches that fork here and end on this row.
		var waiting []int
		for i, l := range lanes {
			if l.want == c.Hash {
				waiting = append(waiting, i)
			}
		}
		before := make([]bool, len(lanes))
		for i, l := range lanes {
			before[i] = l.want != ""
		}

		lane := -1
		if len(waiting) > 0 {
			lane = waiting[0]
		} else {
			// A branch tip nobody was waiting for: new lane, new colour.
			lane = freeLane(nil)
			lanes[lane].color = newColor()
			before = append(before, make([]bool, len(lanes)-len(before))...)
		}
		laneColor := lanes[lane].color

		closing := map[int]bool{}
		for _, i := range waiting[min(1, len(waiting)):] {
			closing[i] = true
			lanes[i].want = ""
		}

		// First parent continues this lane; a root commit ends it.
		if len(c.Parents) > 0 {
			lanes[lane].want = c.Parents[0]
		} else {
			lanes[lane].want = ""
		}

		// Further parents (merge): join a lane already waiting for that
		// parent, or open a new one.
		opened := map[int]bool{}
		joined := map[int]bool{}
		for _, p := range c.Parents[min(1, len(c.Parents)):] {
			target := -1
			for i, l := range lanes {
				if l.want == p && i != lane {
					target = i
					break
				}
			}
			if target >= 0 {
				joined[target] = true
				continue
			}
			busy := map[int]bool{lane: true}
			for i := range closing {
				busy[i] = true
			}
			for i := range opened {
				busy[i] = true
			}
			target = freeLane(busy)
			lanes[target] = laneState{want: p, color: newColor()}
			opened[target] = true
		}

		width := len(lanes)
		for len(before) < width {
			before = append(before, false)
		}

		// Horizontal span of the connectors on this row.
		lo, hi := lane, lane
		for _, set := range []map[int]bool{closing, opened, joined} {
			for i := range set {
				lo, hi = min(lo, i), max(hi, i)
			}
		}

		row := graphRow{cells: make([]graphCell, width), lane: lane}
		for i := 0; i < width; i++ {
			cell := graphCell{glyph: ' ', glyphColor: -1, spacer: ' ', spacerColor: -1}
			inSpan := i >= lo && i <= hi
			color := lanes[i].color
			switch {
			case i == lane:
				cell.glyph, cell.glyphColor = glyphCommit, laneColor
				if c.Hash == head {
					cell.glyph = glyphHead
				}
			case closing[i]:
				cell.glyph, cell.glyphColor = pick(i > lane, '╯', '╰'), color
			case opened[i]:
				cell.glyph, cell.glyphColor = pick(i > lane, '╮', '╭'), color
			case joined[i]:
				cell.glyph, cell.glyphColor = pick(i > lane, '┤', '├'), color
			case before[i] && lanes[i].want != "" && inSpan:
				cell.glyph, cell.glyphColor = '┼', color
			case before[i] && lanes[i].want != "":
				cell.glyph, cell.glyphColor = '│', color
			case inSpan:
				cell.glyph = '─'
			}
			// The spacer after column i is a connector when the span
			// continues to the right of it.
			if i >= lo && i < hi {
				cell.spacer = '─'
				cell.spacerColor = connectorColor(i, lane, closing, opened, joined, lanes, laneColor)
			}
			if cell.glyph == '─' {
				cell.glyphColor = cell.spacerColor
			}
			row.cells[i] = cell
		}
		rows = append(rows, row)

		// Drop free lanes at the right edge so the graph stays narrow.
		for len(lanes) > 0 && lanes[len(lanes)-1].want == "" {
			lanes = lanes[:len(lanes)-1]
		}
	}
	return rows
}

// connectorColor colours a horizontal segment after column i with the
// colour of the lane the connector leads to.
func connectorColor(i, lane int, closing, opened, joined map[int]bool, lanes []laneState, laneColor int) int {
	if i >= lane {
		// Segment heads right: colour of the nearest end to the right.
		for j := i + 1; j < len(lanes); j++ {
			if closing[j] || opened[j] || joined[j] {
				return lanes[j].color
			}
		}
	} else {
		for j := i; j >= 0; j-- {
			if closing[j] || opened[j] || joined[j] {
				return lanes[j].color
			}
		}
	}
	return laneColor
}

func pick(cond bool, a, b rune) rune {
	if cond {
		return a
	}
	return b
}
