<?php

use App\Livewire\Actions\Logout;
use Livewire\Volt\Component;

new class extends Component
{
    /**
     * Log the current user out of the application.
     */
    public function logout(Logout $logout): void
    {
        $logout();

        $this->redirect('/', navigate: true);
    }
}; ?>

<nav x-data="{ open: false }" class="bg-paper border-b border-stone-200">
    <!-- Primary Navigation Menu -->
    <div class="max-w-[1600px] mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex justify-between h-16">
            <div class="flex">
                <!-- Logo -->
                <div class="shrink-0 flex items-center gap-2">
                    <a href="{{ route('dashboard') }}" wire:navigate class="flex items-center gap-2">
                        <img src="{{ asset('images/logo.png') }}" alt="LANHub" class="h-9 w-auto">
                    </a>
                </div>

                <!-- Navigation Links -->
                <div class="hidden space-x-1 sm:ms-10 sm:flex sm:items-center">
                    <x-nav-link :href="route('dashboard')" :active="request()->routeIs('dashboard')" wire:navigate>
                        {{ __('Dashboard') }}
                    </x-nav-link>
                    <x-nav-link :href="route('explorer')" :active="request()->routeIs('explorer')" wire:navigate>
                        {{ __('Explorer') }}
                    </x-nav-link>
                    <x-nav-link :href="route('activity')" :active="request()->routeIs('activity')" wire:navigate>
                        {{ __('Activity') }}
                    </x-nav-link>
                    <x-nav-link :href="route('agents')" :active="request()->routeIs('agents') || request()->routeIs('agents.*')" wire:navigate>
                        {{ __('Agents') }}
                    </x-nav-link>
                    <x-nav-link :href="route('sync-rules')" :active="request()->routeIs('sync-rules')" wire:navigate>
                        {{ __('Sync Rules') }}
                    </x-nav-link>
                    @if (auth()->user()->is_admin)
                        <x-nav-link :href="route('users')" :active="request()->routeIs('users')" wire:navigate>
                            {{ __('Users') }}
                        </x-nav-link>
                        <x-nav-link :href="route('settings')" :active="request()->routeIs('settings')" wire:navigate>
                            {{ __('Settings') }}
                        </x-nav-link>
                    @endif
                </div>
            </div>

            <!-- Dark mode toggle — deliberately outside the sm:-gated
                 containers below so it's reachable at every viewport
                 width, not just desktop. -->
            <div class="flex items-center gap-2" x-data="{ dark: document.documentElement.classList.contains('dark') }">
                <span class="text-xs font-medium transition-colors" :class="dark ? 'text-stone-400' : 'text-stone-700'">Light Mode</span>
                <button
                    type="button"
                    role="switch"
                    :aria-checked="dark.toString()"
                    aria-label="Toggle dark mode"
                    x-on:click="dark = !dark; window.lanhubSetTheme(dark)"
                    :class="dark ? 'bg-tan-500' : 'bg-stone-300'"
                    class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-tan-400 focus:ring-offset-2"
                >
                    <span
                        class="inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform"
                        :class="dark ? 'translate-x-6' : 'translate-x-1'"
                    ></span>
                </button>
                <span class="text-xs font-medium transition-colors" :class="dark ? 'text-stone-800' : 'text-stone-400'">Dark Mode</span>
            </div>

            <!-- Settings Dropdown -->
            <div class="hidden sm:flex sm:items-center sm:ms-2 gap-2">
                <x-dropdown align="right" width="48">
                    <x-slot name="trigger">
                        <button class="inline-flex items-center px-3 py-2 border border-transparent text-sm leading-4 font-medium rounded-md text-stone-500 bg-paper hover:text-stone-700 focus:outline-none transition ease-in-out duration-150">
                            <div x-data="{{ json_encode(['name' => auth()->user()->name]) }}" x-text="name" x-on:profile-updated.window="name = $event.detail.name"></div>

                            <div class="ms-1">
                                <svg class="fill-current h-4 w-4" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20">
                                    <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
                                </svg>
                            </div>
                        </button>
                    </x-slot>

                    <x-slot name="content">
                        <x-dropdown-link :href="route('profile')" wire:navigate>
                            {{ __('Profile') }}
                        </x-dropdown-link>

                        <!-- Authentication -->
                        <button wire:click="logout" class="w-full text-start">
                            <x-dropdown-link>
                                {{ __('Log Out') }}
                            </x-dropdown-link>
                        </button>
                    </x-slot>
                </x-dropdown>
            </div>

            <!-- Hamburger -->
            <div class="-me-2 flex items-center sm:hidden">
                <button @click="open = ! open" class="inline-flex items-center justify-center p-2 rounded-md text-stone-400 hover:text-stone-500 hover:bg-stone-100 focus:outline-none focus:bg-stone-100 focus:text-stone-500 transition duration-150 ease-in-out">
                    <svg class="h-6 w-6" stroke="currentColor" fill="none" viewBox="0 0 24 24">
                        <path :class="{'hidden': open, 'inline-flex': ! open }" class="inline-flex" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
                        <path :class="{'hidden': ! open, 'inline-flex': open }" class="hidden" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                </button>
            </div>
        </div>
    </div>

    <!-- Responsive Navigation Menu -->
    <div :class="{'block': open, 'hidden': ! open}" class="hidden sm:hidden">
        <div class="pt-2 pb-3 space-y-1">
            <x-responsive-nav-link :href="route('dashboard')" :active="request()->routeIs('dashboard')" wire:navigate>
                {{ __('Dashboard') }}
            </x-responsive-nav-link>
            <x-responsive-nav-link :href="route('explorer')" :active="request()->routeIs('explorer')" wire:navigate>
                {{ __('Explorer') }}
            </x-responsive-nav-link>
            <x-responsive-nav-link :href="route('activity')" :active="request()->routeIs('activity')" wire:navigate>
                {{ __('Activity') }}
            </x-responsive-nav-link>
            <x-responsive-nav-link :href="route('agents')" :active="request()->routeIs('agents') || request()->routeIs('agents.*')" wire:navigate>
                {{ __('Agents') }}
            </x-responsive-nav-link>
            <x-responsive-nav-link :href="route('sync-rules')" :active="request()->routeIs('sync-rules')" wire:navigate>
                {{ __('Sync Rules') }}
            </x-responsive-nav-link>
            @if (auth()->user()->is_admin)
                <x-responsive-nav-link :href="route('users')" :active="request()->routeIs('users')" wire:navigate>
                    {{ __('Users') }}
                </x-responsive-nav-link>
                <x-responsive-nav-link :href="route('settings')" :active="request()->routeIs('settings')" wire:navigate>
                    {{ __('Settings') }}
                </x-responsive-nav-link>
            @endif
        </div>

        <!-- Responsive Settings Options -->
        <div class="pt-4 pb-1 border-t border-stone-200">
            <div class="px-4">
                <div class="font-medium text-base text-stone-800" x-data="{{ json_encode(['name' => auth()->user()->name]) }}" x-text="name" x-on:profile-updated.window="name = $event.detail.name"></div>
                <div class="font-medium text-sm text-stone-500">{{ auth()->user()->email }}</div>
            </div>

            <div class="mt-3 space-y-1">
                <x-responsive-nav-link :href="route('profile')" wire:navigate>
                    {{ __('Profile') }}
                </x-responsive-nav-link>

                <!-- Authentication -->
                <button wire:click="logout" class="w-full text-start">
                    <x-responsive-nav-link>
                        {{ __('Log Out') }}
                    </x-responsive-nav-link>
                </button>
            </div>
        </div>
    </div>
</nav>
