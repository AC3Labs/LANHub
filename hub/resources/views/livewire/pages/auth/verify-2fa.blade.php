<?php

use App\Mail\TwoFactorCodeMail;
use App\Models\Setting;
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

    // 'app' = 6-digit authenticator code (or a recovery code), 'email' = emailed code
    public string $method = 'email';

    public bool $mailConfigured = false;

    public function mount(): void
    {
        if (! Session::has('2fa.user_id')) {
            $this->redirect(route('login'), navigate: true);

            return;
        }

        $this->method = Session::get('2fa.method', 'email') === 'app' ? 'app' : 'email';
        $this->mailConfigured = Setting::mailConfigured();
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

        $valid = $user && (Session::get('2fa.method') === 'app'
            ? $user->verifyAuthenticatorCode($this->code)
            : $user->verifyTwoFactorCode($this->code));

        if (! $valid) {
            RateLimiter::hit($key);
            $this->addError('code', 'That code is incorrect or has expired.');

            return;
        }

        RateLimiter::clear($key);
        $user->clearTwoFactorCode();

        $remember = Session::get('2fa.remember', false);
        Session::forget(['2fa.user_id', '2fa.remember', '2fa.method']);

        Auth::login($user, $remember);
        Session::regenerate();

        if ($user->is_admin) {
            \App\Support\PublicUrl::rememberFrom(request());
        }

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

        // An authenticator user asking for an email switches this login over to it
        Session::put('2fa.method', 'email');
        $this->method = 'email';
        $this->code = '';

        session()->flash('status', 'A code has been sent to your email.');
    }
}; ?>

<div>
    <div class="mb-4 text-sm text-stone-500">
        @if ($method === 'app')
            Enter the 6-digit code from your authenticator app to finish logging in.
        @else
            Enter the 10-character code we just emailed you to finish logging in.
        @endif
    </div>

    @if ($method === 'email' && ! $mailConfigured)
        <div class="mb-4 text-sm text-amber-700 bg-amber-50 border border-amber-200 rounded-md p-3">
            Email isn't set up on this hub yet, so this code wasn't actually sent anywhere &mdash; it was written to the server's log instead. Ask whoever runs this hub for it.
        </div>
    @endif

    <x-auth-session-status class="mb-4" :status="session('status')" />

    <form wire:submit="verify">
        <div>
            <x-input-label for="code" :value="$method === 'app' ? 'Authenticator code' : 'Login code'" />
            <x-text-input
                wire:model="code"
                id="code"
                class="block mt-1 w-full font-mono uppercase tracking-widest text-center text-lg"
                type="text"
                maxlength="11"
                :inputmode="$method === 'app' ? 'numeric' : 'text'"
                autofocus
                autocomplete="one-time-code"
            />
            <x-input-error :messages="$errors->get('code')" class="mt-2" />
            @if ($method === 'app')
                <p class="mt-2 text-xs text-stone-500">Lost your phone? Enter one of your recovery codes instead.</p>
            @endif
        </div>

        <div class="flex items-center justify-between mt-4">
            @if ($method === 'email')
                <button type="button" wire:click="resend" class="underline text-sm text-stone-500 hover:text-stone-800">
                    Resend code
                </button>
            @elseif ($mailConfigured)
                <button type="button" wire:click="resend" class="underline text-sm text-stone-500 hover:text-stone-800">
                    Email me a code instead
                </button>
            @else
                <span></span>
            @endif

            <x-primary-button>
                Verify
            </x-primary-button>
        </div>
    </form>
</div>
