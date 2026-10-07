<?php

namespace App\Http\Controllers;

use App\Models\Note;
use Illuminate\Http\RedirectResponse;
use Illuminate\Http\Request;
use Illuminate\View\View;

class NoteController extends Controller
{
    public function index(Request $request): View
    {
        $notes = Note::query()
            ->when($request->string('q')->toString(), fn ($q, $term) => $q->where(
                fn ($q) => $q->where('title', 'like', "%{$term}%")->orWhere('body', 'like', "%{$term}%")
            ))
            ->orderByDesc('pinned')
            ->latest('updated_at')
            ->get();

        return view('notes.index', ['notes' => $notes, 'q' => $request->string('q')->toString()]);
    }

    public function store(Request $request): RedirectResponse
    {
        Note::create($this->validated($request));

        return redirect()->route('notes.index');
    }

    public function update(Request $request, Note $note): RedirectResponse
    {
        $note->update($this->validated($request));

        return redirect()->route('notes.index');
    }

    public function togglePin(Note $note): RedirectResponse
    {
        $note->update(['pinned' => ! $note->pinned]);

        return redirect()->route('notes.index');
    }

    public function destroy(Note $note): RedirectResponse
    {
        $note->delete();

        return redirect()->route('notes.index');
    }

    private function validated(Request $request): array
    {
        return $request->validate([
            'title' => ['required', 'string', 'max:255'],
            'body' => ['nullable', 'string', 'max:20000'],
        ]);
    }
}
