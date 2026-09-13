<?php

namespace App\Livewire\Forms;

use App\Mail\TwoFactorCodeMail;
use App\Models\User;
use Illuminate\Auth\Events\Lockout;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Mail;
use Illuminate\Support\Facades\RateLimiter;
use Illuminate\Support\Facades\Session;
use Illuminate\Support\Str;
use Illuminate\Validation\ValidationException;
use Livewire\Attributes\Validate;
use Livewire\Form;

class LoginForm extends Form
{
    #[Validate('required|string|email')]
    public string $email = '';

    #[Validate('required|string')]
    public string $password = '';

    #[Validate('boolean')]
    public bool $remember = false;

    /**
     * Checks the given credentials WITHOUT logging the user in — every
     * login requires an emailed code (see verify-2fa), so nothing is
     * authenticated yet. On success, generates and emails that code and
     * stashes the pending user id in the session for the verify-2fa page
     * to pick up.
     *
     * @throws ValidationException
     */
    public function authenticate(): void
    {
        $this->ensureIsNotRateLimited();

        if (! Auth::validate($this->only(['email', 'password']))) {
            RateLimiter::hit($this->throttleKey());

            throw ValidationException::withMessages([
                'form.email' => trans('auth.failed'),
            ]);
        }

        RateLimiter::clear($this->throttleKey());

        $user = User::where('email', $this->email)->firstOrFail();

        if ($user->isPending()) {
            throw ValidationException::withMessages([
                'form.email' => 'This account has not been activated yet — check your email for a setup link.',
            ]);
        }

        $code = $user->generateTwoFactorCode();
        Mail::to($user->email)->send(new TwoFactorCodeMail($code));

        Session::put('2fa.user_id', $user->id);
        Session::put('2fa.remember', $this->remember);
    }

    /**
     * Ensure the authentication request is not rate limited.
     */
    protected function ensureIsNotRateLimited(): void
    {
        if (! RateLimiter::tooManyAttempts($this->throttleKey(), 5)) {
            return;
        }

        event(new Lockout(request()));

        $seconds = RateLimiter::availableIn($this->throttleKey());

        throw ValidationException::withMessages([
            'form.email' => trans('auth.throttle', [
                'seconds' => $seconds,
                'minutes' => ceil($seconds / 60),
            ]),
        ]);
    }

    /**
     * Get the authentication rate limiting throttle key.
     */
    protected function throttleKey(): string
    {
        return Str::transliterate(Str::lower($this->email).'|'.request()->ip());
    }
}
