<?php

use App\Models\User;
use Illuminate\Support\Facades\Hash;
use Illuminate\Validation\Rules;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Locked;
use Livewire\Volt\Component;

new #[Layout('layouts.guest')] class extends Component
{
    #[Locked]
    public string $token = '';

    #[Locked]
    public ?User $user = null;

    public string $password = '';

    public string $password_confirmation = '';

    public function mount(string $token): void
    {
        $this->token = $token;

        $this->user = User::where('invite_token', $token)
            ->where('invite_token_expires_at', '>', now())
            ->first();
    }

    public function setPassword(): void
    {
        abort_unless($this->user, 404);

        $this->validate([
            'password' => ['required', 'string', 'confirmed', Rules\Password::defaults()],
        ]);

        $this->user->forceFill([
            'password' => Hash::make($this->password),
            'invite_token' => null,
            'invite_token_expires_at' => null,
            'email_verified_at' => now(),
        ])->save();

        session()->flash('status', 'Your password has been set — you can now log in.');

        $this->redirectRoute('login', navigate: true);
    }
}; ?>

<div>
    @if (! $user)
        <div class="text-sm text-stone-600">
            This invite link is invalid or has expired. Ask whoever invited you to send a new one.
        </div>
    @else
        <p class="text-sm text-stone-500 mb-4">Welcome, {{ $user->name }} — set a password to activate your account.</p>

        <form wire:submit="setPassword">
            <div>
                <x-input-label for="password" value="Password" />
                <x-text-input wire:model="password" id="password" class="block mt-1 w-full" type="password" required autofocus autocomplete="new-password" />
                <x-input-error :messages="$errors->get('password')" class="mt-2" />
            </div>

            <div class="mt-4">
                <x-input-label for="password_confirmation" value="Confirm password" />
                <x-text-input wire:model="password_confirmation" id="password_confirmation" class="block mt-1 w-full" type="password" required autocomplete="new-password" />
                <x-input-error :messages="$errors->get('password_confirmation')" class="mt-2" />
            </div>

            <div class="flex items-center justify-end mt-4">
                <x-primary-button>Set password</x-primary-button>
            </div>
        </form>
    @endif
</div>
