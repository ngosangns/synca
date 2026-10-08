<?php

use App\Http\Controllers\LogController;
use App\Http\Controllers\SyncaController;
use Illuminate\Support\Facades\Route;

Route::get('/', [SyncaController::class, 'index'])->name('dashboard');
Route::get('/logs', [LogController::class, 'feed'])->name('logs.feed');

Route::post('/sync/plan', [SyncaController::class, 'plan'])->name('sync.plan');
Route::post('/sync/apply', [SyncaController::class, 'apply'])->name('sync.apply');

Route::post('/skills/install', [SyncaController::class, 'installSkill'])->name('skills.install');
Route::post('/skills/remove', [SyncaController::class, 'removeSkill'])->name('skills.remove');

Route::post('/mcp/add', [SyncaController::class, 'addMcp'])->name('mcp.add');
Route::post('/mcp/remove', [SyncaController::class, 'removeMcp'])->name('mcp.remove');

Route::post('/update/check', [SyncaController::class, 'updateCheck'])->name('update.check');
Route::post('/update/install', [SyncaController::class, 'updateInstall'])->name('update.install');

Route::post('/settings/project-dir', [SyncaController::class, 'projectDir'])->name('settings.project-dir');
Route::post('/logs/clear', [SyncaController::class, 'clearLogs'])->name('logs.clear');
