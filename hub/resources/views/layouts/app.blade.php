<!DOCTYPE html>
<html lang="{{ str_replace('_', '-', app()->getLocale()) }}">
    <head>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <meta name="csrf-token" content="{{ csrf_token() }}">

        <title>{{ config('app.name', 'LANHub') }}</title>

        <!-- Applied before first paint so there's no flash of the wrong
             theme; the toggle in layout/navigation.blade.php calls
             window.lanhubSetTheme() to flip it afterward. -->
        <script>
            (function () {
                var stored = localStorage.getItem('lanhub-theme');
                var dark = stored ? stored === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches;
                document.documentElement.classList.toggle('dark', dark);
            })();
            window.lanhubSetTheme = function (dark) {
                document.documentElement.classList.toggle('dark', dark);
                localStorage.setItem('lanhub-theme', dark ? 'dark' : 'light');
            };
        </script>

        <link rel="icon" type="image/x-icon" href="{{ asset('favicon.ico') }}">
        <link rel="apple-touch-icon" href="{{ asset('apple-touch-icon.png') }}">

        <!-- Fonts -->
        <link rel="preconnect" href="https://fonts.bunny.net">
        <link href="https://fonts.bunny.net/css?family=figtree:400,500,600,700&display=swap" rel="stylesheet" />

        <!-- Scripts -->
        @vite(['resources/css/app.css', 'resources/js/app.js'])
    </head>
    <body class="font-sans antialiased text-stone-800">
        <div class="min-h-screen flex flex-col bg-stone-100">
            <livewire:layout.navigation />

            @if (isset($header))
                <header class="bg-paper border-b border-stone-200">
                    <div class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
                        {{ $header }}
                    </div>
                </header>
            @endif

            <main class="flex-1">
                {{ $slot }}
            </main>

            <livewire:transfers.panel />

            <x-footer />
        </div>
    </body>
</html>
