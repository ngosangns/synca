<?php

namespace Tests\Feature;

use App\Models\Note;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class NoteTest extends TestCase
{
    use RefreshDatabase;

    public function test_index_lists_pinned_notes_first(): void
    {
        Note::factory()->create(['title' => 'Regular']);
        Note::factory()->create(['title' => 'Pinned', 'pinned' => true]);

        $this->get('/')->assertOk()->assertSeeInOrder(['Pinned', 'Regular']);
    }

    public function test_search_filters_notes(): void
    {
        Note::factory()->create(['title' => 'Groceries']);
        Note::factory()->create(['title' => 'Meeting']);

        $this->get('/?q=Groc')->assertOk()->assertSee('Groceries')->assertDontSee('Meeting');
    }

    public function test_create_update_pin_and_delete(): void
    {
        $this->post('/notes', ['title' => 'First', 'body' => 'Hello'])->assertRedirect('/');
        $note = Note::sole();
        $this->assertSame('Hello', $note->body);

        $this->put("/notes/{$note->id}", ['title' => 'Renamed', 'body' => ''])->assertRedirect('/');
        $this->assertSame('Renamed', $note->fresh()->title);

        $this->patch("/notes/{$note->id}/pin")->assertRedirect('/');
        $this->assertTrue($note->fresh()->pinned);

        $this->delete("/notes/{$note->id}")->assertRedirect('/');
        $this->assertDatabaseCount('notes', 0);
    }

    public function test_title_is_required(): void
    {
        $this->post('/notes', ['title' => ''])->assertSessionHasErrors('title');
    }
}
