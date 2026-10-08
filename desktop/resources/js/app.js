const $ = (selector, root = document) => root.querySelector(selector);

const progress = (active) => $('#page-progress')?.classList.toggle('active', active);

// ---------------------------------------------------------------------------
// Boards region: swap skills/MCP lists + detail pane without a full reload.
// ---------------------------------------------------------------------------
const boards = () => $('#boards-region');
let fetching = false;

const swapBoards = async (url, pushState = true) => {
    if (fetching) return;
    fetching = true;
    const el = boards();
    el?.classList.add('is-loading');
    try {
        const next = new URL(url, location.origin);
        next.searchParams.set('partial', '1');
        const res = await fetch(next);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        if (el) {
            el.innerHTML = await res.text();
            el.classList.remove('is-loading');
        }
        if (pushState) history.pushState(null, '', next.pathname + next.search.replace(/&?partial=1/, ''));
    } catch {
        window.location.href = url;
    } finally {
        fetching = false;
    }
};

document.addEventListener('click', (event) => {
    const link = event.target.closest('a[data-nav]');
    if (!link || event.metaKey || event.ctrlKey || event.shiftKey) return;
    event.preventDefault();
    swapBoards(link.href);
});

// Refresh link: force a real reload so the inventory re-scans.
document.addEventListener('click', (event) => {
    const link = event.target.closest('a[data-refresh]');
    if (!link) return;
    event.preventDefault();
    window.location.reload();
});

window.addEventListener('popstate', () => window.location.reload());

// ---------------------------------------------------------------------------
// Conflict decisions: collect per-item policies into the hidden JSON field.
// ---------------------------------------------------------------------------
document.addEventListener('submit', (event) => {
    const form = event.target.closest('form[data-decisions-form]');
    if (!form) return;
    const out = { skills: {}, mcps: {} };
    form.querySelectorAll('select[data-conflict]').forEach((sel) => {
        const bag = sel.dataset.kind === 'mcps' ? 'mcps' : 'skills';
        out[bag][sel.dataset.key] = sel.value;
    });
    const field = form.querySelector('input[data-decisions-json]');
    if (field) field.value = JSON.stringify(out);
}, true); // capture phase so it runs before the busy-state handler

// ---------------------------------------------------------------------------
// MCP add form: toggle command vs url field by transport.
// ---------------------------------------------------------------------------
document.addEventListener('change', (event) => {
    const sel = event.target.closest('select[data-mcp-transport]');
    if (!sel) return;
    const stdio = sel.value === 'stdio' || sel.value === 'local';
    $('[data-mcp-command]')?.classList.toggle('hidden', !stdio);
    $('[data-mcp-url]')?.classList.toggle('hidden', stdio);
});

// ---------------------------------------------------------------------------
// Busy state on every mutating form submit.
// ---------------------------------------------------------------------------
document.addEventListener('submit', (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) return;
    if ((form.getAttribute('method') || 'get').toLowerCase() === 'get') return;
    const button = form.querySelector('button[type="submit"], button:not([type])');
    if (button && !button.disabled) {
        button.classList.add('is-busy');
        button.setAttribute('aria-busy', 'true');
        button.disabled = true;
    }
});

// ---------------------------------------------------------------------------
// Two-step destructive action: first click arms, second click submits.
// ---------------------------------------------------------------------------
document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-confirm]');
    document.querySelectorAll('[data-confirm].armed').forEach((b) => {
        if (b !== button) {
            b.classList.remove('armed');
            if (b.dataset.armedText && b.dataset.originalText) b.textContent = b.dataset.originalText;
        }
    });
    if (!button) return;
    if (button.classList.contains('armed')) return;
    event.preventDefault();
    if (button.dataset.armedText) {
        button.dataset.originalText = button.textContent.trim();
        button.textContent = button.dataset.armedText;
    }
    button.classList.add('armed');
    setTimeout(() => {
        button.classList.remove('armed');
        if (button.dataset.armedText && button.dataset.originalText) button.textContent = button.dataset.originalText;
    }, 3000);
});

// ---------------------------------------------------------------------------
// Log pane: infinite scroll for older entries + polling for new ones.
// ---------------------------------------------------------------------------
const logMeta = () => {
    const entries = document.querySelectorAll('#logs-list [data-log-entry]');
    return {
        newest: entries.length ? entries[0].dataset.id : '',
        oldest: entries.length ? entries[entries.length - 1].dataset.id : '',
    };
};

const fetchLogs = async (params) => {
    const res = await fetch(`/logs?${params}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const html = await res.text();
    const doc = new DOMParser().parseFromString(html, 'text/html');
    return [...doc.querySelectorAll('[data-log-entry]')];
};

let logLoading = false;
const loadOlderLogs = async () => {
    const { oldest } = logMeta();
    if (!oldest || logLoading) return;
    logLoading = true;
    const tpl = $('#log-skeleton');
    const list = $('#logs-list');
    const skeletons = [];
    if (tpl && list) {
        for (let i = 0; i < 3; i++) {
            const n = tpl.content.firstElementChild.cloneNode(true);
            list.appendChild(n);
            skeletons.push(n);
        }
    }
    try {
        const entries = await fetchLogs(`before=${oldest}`);
        skeletons.forEach((n) => n.remove());
        if (list) entries.forEach((e) => list.appendChild(e));
        if (entries.length === 0) logObserver?.disconnect();
    } catch {
        skeletons.forEach((n) => n.remove());
    } finally {
        logLoading = false;
    }
};

let logObserver;
const initLogScroll = () => {
    const sentinel = $('#logs-sentinel');
    const scroll = $('#logs-scroll');
    if (!sentinel || !scroll) return;
    logObserver ??= new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting)) loadOlderLogs();
    }, { root: scroll, rootMargin: '200px 0px' });
    logObserver.observe(sentinel);
};

// Poll for new entries while the window is visible.
const pollLogs = async () => {
    if (document.hidden || logLoading) return;
    const { newest } = logMeta();
    try {
        const entries = await fetchLogs(newest ? `after=${newest}` : '');
        if (!entries.length) return;
        const list = $('#logs-list');
        const empty = list?.querySelector(':scope > :not([data-log-entry])');
        empty?.remove();
        // entries arrive newest-first; prepend preserving order
        [...entries].reverse().forEach((e) => list?.prepend(e));
    } catch { /* pane stays stale; next poll retries */ }
};

initLogScroll();
setInterval(pollLogs, 5000);

// ---------------------------------------------------------------------------
// TUI-flavoured hotkeys: j/k move · 1/2 scope · r reload · u update · s sync all.
// ---------------------------------------------------------------------------
const isTyping = () => /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');

const navLinks = () => [...document.querySelectorAll('#boards-region a[data-nav]')]
    .filter((a) => new URL(a.href, location.origin).searchParams.get('key'));
const currentNavIndex = (links) => {
    const here = new URL(location.href);
    return links.findIndex((a) => {
        const u = new URL(a.href, location.origin);
        return u.searchParams.get('key') === here.searchParams.get('key')
            && u.searchParams.get('section') === here.searchParams.get('section');
    });
};

document.addEventListener('keydown', (event) => {
    if (isTyping() || event.metaKey || event.ctrlKey || event.altKey) return;
    const key = event.key;
    if (key === 'j' || key === 'k' || key === 'ArrowDown' || key === 'ArrowUp') {
        const links = navLinks();
        if (!links.length) return;
        event.preventDefault();
        const i = currentNavIndex(links);
        const next = key === 'j' || key === 'ArrowDown' ? i + 1 : i - 1;
        const target = links[Math.min(Math.max(next, 0), links.length - 1)];
        if (target && target.href !== location.href) swapBoards(target.href);
    } else if (key === '1' || key === '2') {
        const tab = document.querySelector(`a[data-scope-tab="${key === '1' ? 'user' : 'project'}"]`);
        if (tab) { event.preventDefault(); window.location.href = tab.href; }
    } else if (key === 'r') {
        event.preventDefault();
        window.location.reload();
    } else if (key === 'u') {
        event.preventDefault();
        document.querySelector('form[data-update-form]')?.requestSubmit();
    } else if (key === 's') {
        event.preventDefault();
        document.querySelector('form[data-syncall-form]')?.requestSubmit();
    } else if (key === '?') {
        event.preventDefault();
        const help = $('#help-pop');
        if (help) help.open = !help.open;
    } else if (key === 'Escape') {
        const help = $('#help-pop');
        if (help?.open) help.open = false;
    }
});

// Close the help popover when clicking elsewhere.
document.addEventListener('click', (event) => {
    const help = $('#help-pop');
    if (help?.open && !event.target.closest('#help-pop')) help.open = false;
});

// ---------------------------------------------------------------------------
// Auto-dismiss flash messages.
// ---------------------------------------------------------------------------
const flash = document.querySelector('[data-flash]');
if (flash) {
    setTimeout(() => {
        flash.classList.add('opacity-0', '-translate-y-1');
        setTimeout(() => flash.remove(), 300);
    }, 3200);
}
