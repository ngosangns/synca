const $ = (selector, root = document) => root.querySelector(selector);
const isTyping = () => /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');
const region = () => $('#notes-region');
const progress = (active) => $('#page-progress')?.classList.toggle('active', active);

// "/" or Cmd/Ctrl+K focuses search.
document.addEventListener('keydown', (event) => {
    const slash = event.key === '/' && !isTyping();
    const cmdK = event.key.toLowerCase() === 'k' && (event.metaKey || event.ctrlKey);
    if (!slash && !cmdK) return;
    const search = $('[data-search-input]');
    if (search) {
        event.preventDefault();
        search.focus();
        search.select();
    }
});

let fetching = false;

const fetchRegion = async (url) => {
    const res = await fetch(url, { headers: { 'X-Requested-With': 'fetch' } });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.text();
};

// After a region swap: drop the no-JS pagination nav and re-arm infinite scroll.
const initRegion = () => {
    region()?.querySelector('[data-pagination]')?.remove();
    observeSentinel();
};

const swapRegion = async (url, pushState = true) => {
    if (fetching) return;
    fetching = true;
    progress(true);
    region()?.classList.add('is-loading');
    try {
        const html = await fetchRegion(url);
        const el = region();
        el.innerHTML = html;
        el.classList.remove('is-loading');
        if (pushState) {
            const visible = new URL(url, location.origin);
            visible.searchParams.delete('partial');
            history.pushState(null, '', visible);
        }
        initRegion();
    } catch {
        window.location.href = url;
    } finally {
        progress(false);
        fetching = false;
    }
};

// Search: debounced fetch that swaps the notes region without a full reload.
const searchInput = $('[data-search-input]');
if (searchInput) {
    let timer;
    const submit = () => {
        const url = new URL(searchInput.form.action);
        const q = searchInput.value.trim();
        q ? url.searchParams.set('q', q) : url.searchParams.delete('q');
        url.searchParams.delete('page');
        url.searchParams.set('partial', '1');
        swapRegion(url);
    };
    searchInput.addEventListener('input', () => {
        clearTimeout(timer);
        timer = setTimeout(submit, 350);
    });
    searchInput.form.addEventListener('submit', (event) => {
        event.preventDefault();
        clearTimeout(timer);
        submit();
    });
}

// "Clear search" links act like the search form (no full reload).
document.addEventListener('click', (event) => {
    const link = event.target.closest('[data-clear-search]');
    if (!link) return;
    event.preventDefault();
    if (searchInput) searchInput.value = '';
    const url = new URL(link.href);
    url.searchParams.set('partial', '1');
    swapRegion(url);
    searchInput?.focus();
});

// Infinite scroll: fetch the next page when the sentinel enters the viewport.
let observer;
const observeSentinel = () => {
    const sentinel = $('#notes-sentinel');
    if (!sentinel) return;
    observer ??= new IntersectionObserver((entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
    }, { rootMargin: '600px 0px' });
    observer.disconnect();
    observer.observe(sentinel);
};

const skeletonCount = 4;
const showSkeletons = () => {
    const tpl = $('#note-skeleton');
    const notesEl = $('#notes');
    if (!tpl || !notesEl) return () => {};
    const frag = document.createDocumentFragment();
    const nodes = [];
    for (let i = 0; i < skeletonCount; i++) {
        const node = tpl.content.firstElementChild.cloneNode(true);
        frag.appendChild(node);
        nodes.push(node);
    }
    notesEl.appendChild(frag);
    return () => nodes.forEach((n) => n.remove());
};

const loadMore = async () => {
    const meta = $('#notes-meta');
    const url = meta?.dataset.nextUrl;
    if (!url || fetching) return;
    fetching = true;
    const removeSkeletons = showSkeletons();
    try {
        const html = await fetchRegion(`${url}&partial=1`);
        const doc = new DOMParser().parseFromString(html, 'text/html');
        removeSkeletons();
        const notesEl = $('#notes');
        const frag = document.createDocumentFragment();
        doc.querySelectorAll('#notes > *').forEach((node) => frag.appendChild(node));
        notesEl?.appendChild(frag);
        meta.dataset.nextUrl = doc.querySelector('#notes-meta')?.dataset.nextUrl || '';
        if (!meta.dataset.nextUrl) observer?.disconnect();
    } catch {
        removeSkeletons();
    } finally {
        fetching = false;
    }
};

// Back/forward falls back to a server render.
window.addEventListener('popstate', () => window.location.reload());

// Busy state on every mutating form (GET forms are handled by fetch above).
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

// Keyboard activation for label-based toggles (peer checkbox disclosures).
document.addEventListener('keydown', (event) => {
    const toggle = event.target.closest('label[role="button"][for]');
    if (!toggle || (event.key !== 'Enter' && event.key !== ' ')) return;
    event.preventDefault();
    toggle.click();
});

// Two-step delete: first click arms the button, second click submits.
document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-confirm]');
    document.querySelectorAll('[data-confirm].armed').forEach((b) => {
        if (b !== button) b.classList.remove('armed');
    });
    if (!button) return;
    if (button.classList.contains('armed')) return;
    event.preventDefault();
    button.classList.add('armed');
    setTimeout(() => button.classList.remove('armed'), 3000);
});

// Auto-dismiss flash messages.
const flash = document.querySelector('[data-flash]');
if (flash) {
    setTimeout(() => {
        flash.classList.add('opacity-0', '-translate-y-1');
        setTimeout(() => flash.remove(), 300);
    }, 2800);
}

initRegion();
