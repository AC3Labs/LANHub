<?php

use Illuminate\Support\Facades\Route;
use Livewire\Volt\Volt;

Route::middleware('guest')->group(function () {
    Volt::route('login', 'pages.auth.login')
        ->name('login');

    Volt::route('verify-2fa', 'pages.auth.verify-2fa')
        ->name('verify-2fa');

    Volt::route('forgot-password', 'pages.auth.forgot-password')
        ->name('password.request');

    Volt::route('reset-password/{token}', 'pages.auth.reset-password')
        ->name('password.reset');

    Volt::route('set-password/{token}', 'pages.auth.set-password')
        ->name('set-password');
});
