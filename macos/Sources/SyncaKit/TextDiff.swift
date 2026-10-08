import Foundation

public struct DiffRow: Sendable, Hashable {
    public enum Kind: Sendable { case same, removed, added, collapsed }
    public let kind: Kind
    public let oldNo: Int?
    public let newNo: Int?
    /// Line text; for `.collapsed` rows a count is in `hidden` instead.
    public let text: String
    public let hidden: Int
}

/// One aligned row of a side-by-side view.
public struct SplitRow: Sendable, Hashable {
    public let left: DiffRow?
    public let right: DiffRow?
    /// Non-nil for a "N unchanged lines" separator.
    public let collapsed: Int?
}

/// Line diff (prefix/suffix trim + LCS) with context collapsing.
public enum TextDiff {
    /// Above this many cells in the LCS table the middle is treated as replaced.
    static let maxCells = 4_000_000

    public static func lines(of text: String) -> [String] {
        if text.isEmpty { return [] }
        // "\r\n" is one Character in Swift, so split on the Unicode scalars.
        var parts: [String] = []
        var cur = String.UnicodeScalarView()
        var sawLF = false
        for u in text.unicodeScalars {
            if u == "\n" {
                if cur.last == "\r" { cur.removeLast() }
                parts.append(String(cur)); cur = .init(); sawLF = true
            } else { cur.append(u) }
        }
        if !cur.isEmpty || !sawLF { parts.append(String(cur)) }
        return parts
    }

    /// Full row list (no collapsing).
    public static func rows(old: [String], new: [String]) -> [DiffRow] {
        var pre = 0
        while pre < old.count, pre < new.count, old[pre] == new[pre] { pre += 1 }
        var suf = 0
        while suf < old.count - pre, suf < new.count - pre,
              old[old.count - 1 - suf] == new[new.count - 1 - suf] { suf += 1 }
        let a = Array(old[pre..<(old.count - suf)])
        let b = Array(new[pre..<(new.count - suf)])

        var out: [DiffRow] = []
        func same(_ o: Int, _ n: Int, _ t: String) {
            out.append(DiffRow(kind: .same, oldNo: o + 1, newNo: n + 1, text: t, hidden: 0))
        }
        for i in 0..<pre { same(i, i, old[i]) }

        var i = 0, j = 0
        let n = a.count, m = b.count
        func removed(_ k: Int) { out.append(DiffRow(kind: .removed, oldNo: pre + k + 1, newNo: nil, text: a[k], hidden: 0)) }
        func added(_ k: Int) { out.append(DiffRow(kind: .added, oldNo: nil, newNo: pre + k + 1, text: b[k], hidden: 0)) }

        if n == 0 || m == 0 || n * m > maxCells {
            for k in 0..<n { removed(k) }
            for k in 0..<m { added(k) }
        } else {
            // lcs[i][j] = LCS length of a[i...] and b[j...]
            let w = m + 1
            var lcs = [UInt16](repeating: 0, count: (n + 1) * w)
            for x in stride(from: n - 1, through: 0, by: -1) {
                for y in stride(from: m - 1, through: 0, by: -1) {
                    lcs[x * w + y] = a[x] == b[y] ? lcs[(x + 1) * w + y + 1] + 1
                        : max(lcs[(x + 1) * w + y], lcs[x * w + y + 1])
                }
            }
            while i < n, j < m {
                if a[i] == b[j] { same(pre + i, pre + j, a[i]); i += 1; j += 1 }
                else if lcs[(i + 1) * w + j] >= lcs[i * w + j + 1] { removed(i); i += 1 }
                else { added(j); j += 1 }
            }
            while i < n { removed(i); i += 1 }
            while j < m { added(j); j += 1 }
        }
        for k in 0..<suf {
            same(old.count - suf + k, new.count - suf + k, old[old.count - suf + k])
        }
        return out
    }

    /// Replaces long unchanged runs with a single `.collapsed` row, keeping
    /// `context` lines around every change.
    public static func collapse(_ rows: [DiffRow], context: Int = 3) -> [DiffRow] {
        guard rows.contains(where: { $0.kind != .same }) else {
            return rows.isEmpty ? [] : [DiffRow(kind: .collapsed, oldNo: nil, newNo: nil, text: "", hidden: rows.count)]
        }
        var keep = [Bool](repeating: false, count: rows.count)
        for (idx, r) in rows.enumerated() where r.kind != .same {
            for k in max(0, idx - context)...min(rows.count - 1, idx + context) { keep[k] = true }
        }
        var out: [DiffRow] = []
        var hidden = 0
        for (idx, r) in rows.enumerated() {
            if keep[idx] {
                if hidden > 0 { out.append(DiffRow(kind: .collapsed, oldNo: nil, newNo: nil, text: "", hidden: hidden)); hidden = 0 }
                out.append(r)
            } else { hidden += 1 }
        }
        if hidden > 0 { out.append(DiffRow(kind: .collapsed, oldNo: nil, newNo: nil, text: "", hidden: hidden)) }
        return out
    }

    public static func diff(old: String, new: String, context: Int = 3) -> [DiffRow] {
        collapse(rows(old: lines(of: old), new: lines(of: new)), context: context)
    }

    /// Aligns removed/added blocks side by side (pairing them line by line).
    public static func split(_ rows: [DiffRow]) -> [SplitRow] {
        var out: [SplitRow] = []
        var i = 0
        while i < rows.count {
            let r = rows[i]
            switch r.kind {
            case .same: out.append(SplitRow(left: r, right: r, collapsed: nil)); i += 1
            case .collapsed: out.append(SplitRow(left: nil, right: nil, collapsed: r.hidden)); i += 1
            case .removed, .added:
                var rem: [DiffRow] = [], add: [DiffRow] = []
                while i < rows.count, rows[i].kind == .removed || rows[i].kind == .added {
                    if rows[i].kind == .removed { rem.append(rows[i]) } else { add.append(rows[i]) }
                    i += 1
                }
                for k in 0..<max(rem.count, add.count) {
                    out.append(SplitRow(left: k < rem.count ? rem[k] : nil,
                                        right: k < add.count ? add[k] : nil, collapsed: nil))
                }
            }
        }
        return out
    }
}
