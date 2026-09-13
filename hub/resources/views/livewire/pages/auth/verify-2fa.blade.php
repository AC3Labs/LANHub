<?php

use App\Mail\TwoFactorCodeMail;
use App\Models\User;
use Illuminate\Auth\Events\Lockout;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Mail;
use Illuminate\Support\Facades\RateLimiter;
use Illuminate\Support\Facades\Session;
use Livewire\Attributes\Layout;
use Livewire\Volt\Component;

new #[Layout('layouts.guest')] class extends Component
{
    public string $code = '';

    public function mount(): void
    {
        if (! Session::has('2fa.user_id')) {
            $this->redirect(route('login'), navigate: true);
        }
    }

    public function verify(): void
    {
        $this->validate(['code' => 'required|string']);

        $key = 'two-factor:'.Session::get('2fa.user_id');

        if (RateLimiter::tooManyAttempts($key, 5)) {
            event(new Lockout(request()));
            $seconds = RateLimiter::availableIn($key);
            $this->addError('code', "Too many attempts. Try again in {$seconds} seconds.");

            return;
        }

        $user = User::find(Session::get('2fa.user_id'));

        if (! $user || ! $user->verifyTwoFactorCode($this->code)) {
            RateLimiter::hit($key);
            $this->addError('code', 'That code is incorrect or has expired.');

            return;
        }

        RateLimiter::clear($key);
        $user->clearTwoFactorCode();

        $remember = Session::get('2fa.remember', false);
        Session::forget(['2fa.user_id', '2fa.remember']);

        Auth::login($user, $remember);
        Session::regenerate();

        $this->redirect(route('dashboard'), navigate: true);
    }

    public function resend(): void
    {
        $user = User::find(Session::get('2fa.user_id'));

        if (! $user) {
            $this->redirect(route('login'), navigate: true);

            return;
        }

        $newCode = $user->generateTwoFactorCode();
        Mail::to($user->email)->send(new TwoFactorCodeMail($newCode));

        session()->flash('status', 'A new code has been sent to your email.');
    }
}; ?>

<div>
    <div class="mb-4 text-sm text-stone-500">
        Enter the 10-character code we just emailed you to finish logging in.
    </div>

    <x-auth-session-status class="mb-4" :status="session('status')" />

    <form wire:submit="verify">
        <div>
            <x-input-label for="code" value="Login code" />
            <x-text-input
                wire:model="code"
                id="code"
                class="block mt-1 w-full font-mono uppercase tracking-widest text-center text-lg"
                type="text"
                maxlength="10"
                autofocus
                autocomplete="one-time-code"
            />
            <x-input-error :messages="$errors->get('code')" class="mt-2" />
        </div>

        <div class="flex items-center justify-between mt-4">
            <button type="button" wire:click="resend" class="underline text-sm text-stone-500 hover:text-stone-800">
                Resend code
            </button>

            <x-primary-button>
                Verify
            </x-primary-button>
        </div>
    </form>
</div>
