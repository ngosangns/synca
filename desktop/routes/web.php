<?php

use App\Http\Controllers\NoteController;
use Illuminate\Support\Facades\Route;

Route::get('/', [NoteController::class, 'index'])->name('notes.index');
Route::post('/notes', [NoteController::class, 'store'])->name('notes.store');
Route::put('/notes/{note}', [NoteController::class, 'update'])->name('notes.update');
Route::patch('/notes/{note}/pin', [NoteController::class, 'togglePin'])->name('notes.pin');
Route::delete('/notes/{note}', [NoteController::class, 'destroy'])->name('notes.destroy');
