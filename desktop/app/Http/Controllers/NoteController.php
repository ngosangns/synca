<?php

namespace App\Http\Controllers;

use App\Models\Note;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\View\View;

class NoteController extends Controller
{
    private const PER_PAGE = 24;

    public function index(Request $request): View
    {
        $term = $request->string('q')->trim()->toString();

        $notes = Note::query()
            ->when($term, fn ($q) => $q->where(
                fn ($q) => $q->where('title', 'like', "%{$term}%")->orWhere('body', 'like', "%{$term}%")
            ))
            ->orderByDesc('pinned')
            ->latest('updated_at')
            ->paginate(self::PER_PAGE)
            ->withQueryString();

        $data = ['notes' => $notes, 'q' => $term];

        return $request->boolean('partial')
            ? view('notes._region', $data)
            : view('notes.index', $data);
    }

    public function store(Request $request): RedirectResponse
    {
        Note::create($this->validated($request));

        return back()->with('status', 'Note added.');
    }

    public function update(Request $request, Note $note): RedirectResponse
    {
        $note->update($this->validated($request));

        return back()->with('status', 'Note saved.');
    }

    public function togglePin(Note $note): RedirectResponse
    {
        $note->update(['pinned' => ! $note->pinned]);

        return back()->with('status', $note->pinned ? 'Note pinned.' : 'Note unpinned.');
    }

    public function destroy(Note $note): RedirectResponse
    {
        $note->delete();

        return back()->with('status', 'Note deleted.');
    }

    private function validated(Request $request): array
    {
        return $request->validate([
            'title' => ['required', 'string', 'max:255'],
            'body' => ['nullable', 'string', 'max:20000'],
        ]);
    }
}
