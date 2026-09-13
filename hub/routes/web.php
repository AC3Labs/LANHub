<?php

use App\Http\Controllers\PreviewController;
use App\Livewire\Dashboard\Index;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Route;

// No marketing/welcome page — this is a private, invite-only tool.
// Straight to the dashboard if logged in, straight to the branded login
// page otherwise.
Route::get('/', fn () => redirect()->route(Auth::check() ? 'dashboard' : 'login'));

Route::get('dashboard', Index::class)
    ->middleware(['auth'])
    ->name('dashboard');

Route::view('profile', 'profile')
    ->middleware(['auth'])
    ->name('profile');

Route::get('explorer', App\Livewire\Explorer\Index::class)
    ->middleware(['auth'])
    ->name('explorer');

Route::get('machines', App\Livewire\Machines\Index::class)
    ->middleware(['auth'])
    ->name('machines');

Route::get('activity', App\Livewire\Activity\Index::class)
    ->middleware(['auth'])
    ->name('activity');

Route::get('sync-rules', App\Livewire\SyncRules\Index::class)
    ->middleware(['auth'])
    ->name('sync-rules');

Route::get('machines/{machine}/preview', [PreviewController::class, 'show'])
    ->middleware(['auth'])
    ->name('preview');

Route::get('users', App\Livewire\Users\Index::class)
    ->middleware(['auth', 'admin'])
    ->name('users');

Route::get('settings', App\Livewire\Settings\Index::class)
    ->middleware(['auth', 'admin'])
    ->name('settings');

require __DIR__.'/auth.php';
