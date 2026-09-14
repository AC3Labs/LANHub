<?php

namespace App\Livewire\Setup;

use App\Models\User;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Hash;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

/**
 * The in-browser replacement for `php artisan lanhub:create-admin` — a
 * fresh install otherwise has zero users and, since self-registration is
 * disabled, no way to ever log in. This is only reachable while that's
 * still true (see mount()): once any account exists, it has nothing left
 * to do and would just be a second, unauthenticated way to create an
 * admin.
 */
#[Layout('layouts.guest')]
class Index extends Component
{
    #[Validate('required|string|max:255')]
    public string $name = '';

    #[Validate('required|string|email|max:255')]
    public string $email = '';

    #[Validate('required|string|min:8|confirmed')]
    public string $password = '';

    public string $password_confirmation = '';

    public function mount(): void
    {
        if (User::query()->exists()) {
            $this->redirect(route('login'), navigate: true);
        }
    }

    public function create(): void
    {
        // Re-checked right before writing, not just in mount() — closes
        // the window where two people could load this page before
        // either one submits.
        if (User::query()->exists()) {
            $this->redirect(route('login'), navigate: true);

            return;
        }

        $this->validate();

        $user = User::create([
            'name' => $this->name,
            'email' => $this->email,
            'password' => Hash::make($this->password),
        ]);
        $user->forceFill(['is_admin' => true, 'email_verified_at' => now()])->save();

        // Skips the emailed 2FA code every other login requires — SMTP
        // isn't configured yet at this exact point (that's the very next
        // step), so there'd be nowhere for the code to go. Creating this
        // account from inside the browser here is the same trust
        // boundary as running `lanhub:create-admin` on the server
        // directly, which also logs straight in with no 2FA step.
        Auth::login($user);
        session()->regenerate();

        session()->flash('status', 'Your account is ready. Configure outgoing email below so invites and login codes can actually be delivered — or skip it and set this up later.');

        $this->redirect(route('settings'), navigate: true);
    }

    public function render()
    {
        return view('livewire.setup.index');
    }
}
