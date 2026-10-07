const isTyping = () => /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');

// "/" or Cmd/Ctrl+K focuses search.
document.addEventListener('keydown', (event) => {
    const slash = event.key === '/' && !isTyping();
    const cmdK = event.key.toLowerCase() === 'k' && (event.metaKey || event.ctrlKey);
    if (!slash && !cmdK) return;
    const search = document.querySelector('[data-search-input]');
    if (search) {
        event.preventDefault();
        search.focus();
        search.select();
    }
});

// Debounced auto-submit for the search form.
const searchInput = document.querySelector('[data-search-input]');
if (searchInput) {
    let timer;
    searchInput.addEventListener('input', () => {
        clearTimeout(timer);
        timer = setTimeout(() => searchInput.form?.requestSubmit(), 350);
    });
}

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
