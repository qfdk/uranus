// Find text in a ghostty-web terminal, which has no search addon.
// The control center uses a copy of this file (src/components/mqtt-terminal/search.js).
//
// Lines are read cell by cell: a wide character (Chinese...) fills two cells,
// rows wrapped by the terminal are joined, and a match is shown by selecting it
// and scrolling it into view. The buffer changes with every output, so each
// step searches again.

// Logical lines of the active buffer: text plus, for each character, its row
// (from the top of the scrollback) and its first and last cell.
function readLines(term) {
    const buffer = term.buffer.active;
    // ghostty-web 0.4.0 reports no wrapping for scrollback rows: there, a row
    // that reaches the last column is taken as continued on the next one.
    const scrollback = buffer.type === 'normal' ? term.getScrollbackLength() : 0;
    const lines = [];
    let current = null;
    let previousFull = false;
    for (let y = 0; y < buffer.length; y++) {
        const line = buffer.getLine(y);
        if (!line) continue;
        const continued = line.isWrapped || (y < scrollback && previousFull);
        if (!current || !continued) {
            current = {text: '', cells: []};
            lines.push(current);
        }
        const lastCell = line.getCell(line.length - 1);
        previousFull = !!lastCell && (lastCell.getWidth() === 0 || lastCell.getChars() !== '');
        for (let x = 0; x < line.length; x++) {
            const cell = line.getCell(x);
            const width = cell ? cell.getWidth() : 1;
            if (width === 0) continue; // second half of a wide character
            const chars = (cell && cell.getChars()) || ' ';
            for (let i = 0; i < chars.length; i++) {
                current.cells.push({row: y, start: x, end: x + Math.max(width, 1) - 1});
            }
            current.text += chars;
        }
    }
    return lines;
}

function findAll(term, query) {
    const needle = query.toLowerCase();
    const matches = [];
    if (!needle) return matches;
    for (const {text, cells} of readLines(term)) {
        // Lower-casing keeps the length for the scripts used in a terminal.
        const haystack = text.toLowerCase();
        for (let i = haystack.indexOf(needle); i !== -1; i = haystack.indexOf(needle, i + needle.length)) {
            const first = cells[i];
            const last = cells[i + needle.length - 1];
            if (first && last) matches.push({row: first.row, col: first.start, endRow: last.row, endCol: last.end});
        }
    }
    return matches;
}

// Scrolls match into view (centered when it was outside) and selects it.
function reveal(term, match) {
    const scrollback = term.getScrollbackLength();
    let top = scrollback - Math.round(term.getViewportY());
    if (match.row < top || match.endRow >= top + term.rows) {
        top = Math.min(scrollback, Math.max(0, match.row - Math.floor(term.rows / 2)));
        term.scrollToLine(scrollback - top);
    }
    select(term, match);
}

// Selects a match given in rows from the top of the scrollback. term.select()
// can't be used: ghostty-web 0.4.0 adds the scroll offset to the viewport row
// the wrong way round and selects another line as soon as there is scrollback.
// Its selection manager stores the same rows as this file, so the selection is
// set there.
function select(term, match) {
    const selection = term.selectionManager;
    if (!selection) return;
    selection.clearSelection();
    selection.selectionStart = {col: match.col, absoluteRow: match.row};
    selection.selectionEnd = {col: match.endCol, absoluteRow: match.endRow};
    selection.selectionChangedEmitter.fire();
}

export function createSearch(term) {
    let last = null; // the match shown last, kept across new output

    // Searches for query and shows the next match: upwards (towards older
    // output) by default, downwards when forward is true. Starts from the
    // bottom. Returns {index, count}, index -1 when nothing was found.
    function step(query, forward = false) {
        const matches = findAll(term, query);
        if (!matches.length) {
            term.clearSelection();
            last = null;
            return {index: -1, count: 0};
        }
        let index;
        if (!last) {
            index = forward ? 0 : matches.length - 1;
        } else {
            const position = (m) => m.row * term.cols + m.col;
            const from = position(last);
            index = forward
                ? matches.findIndex((m) => position(m) > from)
                : matches.findLastIndex((m) => position(m) < from);
            if (index === -1) index = forward ? 0 : matches.length - 1; // wrap around
        }
        last = matches[index];
        reveal(term, last);
        return {index, count: matches.length};
    }

    // Forget the current match: the next step starts from the bottom again.
    function reset() {
        last = null;
        term.clearSelection();
    }

    return {step, reset};
}
