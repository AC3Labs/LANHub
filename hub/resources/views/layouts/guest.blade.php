<!DOCTYPE html>
<html lang="{{ str_replace('_', '-', app()->getLocale()) }}">
    <head>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <meta name="csrf-token" content="{{ csrf_token() }}">

        <title>{{ config('app.name', 'LANHub') }}</title>

        <!-- Reapplied on livewire:navigated — see layouts/app.blade.php for why. -->
        <script>
            function lanhubApplyTheme() {
                var stored = localStorage.getItem('lanhub-theme');
                var dark = stored ? stored === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches;
                document.documentElement.classList.toggle('dark', dark);
            }
            lanhubApplyTheme();
            document.addEventListener('livewire:navigated', lanhubApplyTheme);
        </script>

        <link rel="icon" type="image/x-icon" href="{{ asset('favicon.ico') }}">
        <link rel="apple-touch-icon" href="{{ asset('apple-touch-icon.png') }}">

        <!-- Fonts -->
        <link rel="preconnect" href="https://fonts.bunny.net">
        <link href="https://fonts.bunny.net/css?family=figtree:400,500,600&display=swap" rel="stylesheet" />

        <!-- Scripts -->
        @vite(['resources/css/app.css', 'resources/js/app.js'])
    </head>
    <body class="font-sans text-stone-800 antialiased">
        <div class="min-h-screen flex flex-col items-center pt-6 sm:pt-0 bg-stone-100">
            <div class="flex-1 flex flex-col items-center justify-center">
                <div>
                    <a href="/" wire:navigate>
                        <img src="{{ asset('images/logo.png') }}" alt="LANHub" class="h-16 w-auto">
                    </a>
                </div>

                <div class="w-full sm:max-w-md mt-6 px-6 py-4 bg-paper shadow-panel overflow-hidden sm:rounded-lg border border-stone-200">
                    {{ $slot }}
                </div>
            </div>

            <x-footer />
        </div>
    </body>
</html>
