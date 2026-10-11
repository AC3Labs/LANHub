<?php

use App\Models\Setting;
use App\Support\Authenticator;
use Illuminate\Support\Facades\Auth;
use Illuminate\Validation\ValidationException;
use Livewire\Volt\Component;

new class extends Component
{
    // 6-digit code typed while confirming setup
    public string $code = '';

    // Shown once, right after setup (or after making new ones)
    public array $recoveryCodes = [];

    public string $current_password = '';

    public bool $confirmingRemoval = false;

    public function startSetup(): void
    {
        $user = Auth::user();

        if ($user->hasAuthenticator()) {
            return;
        }

        // Saved unconfirmed: it only starts being asked for at login once
        // the user proves their app works by entering a code below.
        $user->forceFill([
            'two_factor_secret' => Authenticator::newSecret(),
            'two_factor_confirmed_at' => null,
            'two_factor_recovery_codes' => null,
            'two_factor_last_used_step' => null,
        ])->save();

        $this->reset('code', 'recoveryCodes');
    }

    public function confirmSetup(): void
    {
        $this->resetErrorBag('code');
        $user = Auth::user();

        if (! $user->two_factor_secret || $user->hasAuthenticator()) {
            return;
        }

        $step = Authenticator::verify($user->two_factor_secret, $this->code);

        if ($step === null) {
            $this->addError('code', 'That code didn\'t match. Check the time on your phone is set automatically, and try the newest code.');

            return;
        }

        $this->recoveryCodes = Authenticator::newRecoveryCodes();

        $user->forceFill([
            'two_factor_confirmed_at' => now(),
            'two_factor_last_used_step' => $step,
            'two_factor_recovery_codes' => $this->recoveryCodes,
        ])->save();

        $this->reset('code');
        $this->dispatch('authenticator-updated');
    }

    public function cancelSetup(): void
    {
        $user = Auth::user();

        if (! $user->hasAuthenticator()) {
            $user->removeAuthenticator();
        }

        $this->reset('code');
    }

    public function regenerateRecoveryCodes(): void
    {
        $user = Auth::user();

        if (! $user->hasAuthenticator()) {
            return;
        }

        $this->recoveryCodes = Authenticator::newRecoveryCodes();
        $user->forceFill(['two_factor_recovery_codes' => $this->recoveryCodes])->save();
    }

    public function remove(): void
    {
        try {
            $this->validate(['current_password' => ['required', 'string', 'current_password']]);
        } catch (ValidationException $e) {
            $this->reset('current_password');

            throw $e;
        }

        Auth::user()->removeAuthenticator();

        $this->reset('current_password', 'confirmingRemoval', 'recoveryCodes');
        $this->dispatch('authenticator-updated');
    }

    public function with(): array
    {
        $user = Auth::user();
        $settingUp = $user->two_factor_secret && ! $user->hasAuthenticator();

        return [
            'enabled' => $user->hasAuthenticator(),
            'settingUp' => $settingUp,
            'qrCode' => $settingUp ? Authenticator::qrCodeSvg($user->email, $user->two_factor_secret) : null,
            'setupKey' => $settingUp ? trim(chunk_split($user->two_factor_secret, 4, ' ')) : null,
            'remainingCodes' => count($user->two_factor_recovery_codes ?? []),
            'mailConfigured' => Setting::mailConfigured(),
        ];
    }
}; ?>

<section id="authenticator">
    <header>
        <h2 class="text-lg font-medium text-gray-900 dark:text-gray-100">
            Authenticator app
        </h2>

        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            Log in with a 6-digit code from an app on your phone &mdash; Google Authenticator, Microsoft Authenticator, Authy, 1Password and similar all work &mdash; instead of waiting for an emailed code.
        </p>
    </header>

    <div class="mt-6 space-y-4">
        @if ($enabled)
            <p class="text-sm font-medium text-green-600">
                On. You'll be asked for a code from your app each time you log in.
            </p>

            @if ($recoveryCodes)
                <div class="rounded-md border border-stone-200 bg-stone-50 p-4">
                    <p class="text-sm font-medium text-stone-800">Save these recovery codes somewhere safe.</p>
                    <p class="mt-1 text-xs text-stone-500">If you lose your phone, each one logs you in once. They won't be shown again.</p>
                    <div class="mt-3 grid grid-cols-2 gap-2 font-mono text-sm text-stone-800">
                        @foreach ($recoveryCodes as $recoveryCode)
                            <span>{{ $recoveryCode }}</span>
                        @endforeach
                    </div>
                </div>
            @else
                <p class="text-xs text-stone-500">{{ $remainingCodes }} unused recovery code{{ $remainingCodes === 1 ? '' : 's' }} left.</p>
            @endif

            @if ($confirmingRemoval)
                <form wire:submit="remove" class="space-y-3">
                    @unless ($mailConfigured)
                        <p class="text-sm text-amber-700">Email isn't set up on this hub. Without the app you won't be able to receive a login code, so you'd be locked out the next time you log out.</p>
                    @endunless
                    <div>
                        <x-input-label for="authenticator_current_password" value="Confirm with your password" />
                        <x-text-input wire:model="current_password" id="authenticator_current_password" type="password" class="mt-1 block w-full" autocomplete="current-password" />
                        <x-input-error :messages="$errors->get('current_password')" class="mt-2" />
                    </div>
                    <div class="flex items-center gap-4">
                        <x-danger-button>Turn off</x-danger-button>
                        <button type="button" wire:click="$set('confirmingRemoval', false)" class="text-sm text-stone-500 hover:text-stone-800">Cancel</button>
                    </div>
                </form>
            @else
                <div class="flex items-center gap-4">
                    <x-secondary-button wire:click="regenerateRecoveryCodes">New recovery codes</x-secondary-button>
                    <button type="button" wire:click="$set('confirmingRemoval', true)" class="text-sm text-red-600 hover:text-red-800">Turn off</button>
                </div>
            @endif
        @elseif ($settingUp)
            <ol class="list-decimal ms-5 space-y-1 text-sm text-stone-700">
                <li>Open your authenticator app and add a new account.</li>
                <li>Scan this QR code, or type in the key below it.</li>
                <li>Enter the 6-digit code the app shows.</li>
            </ol>

            <div class="inline-block rounded-md bg-white p-3 border border-stone-200">{!! $qrCode !!}</div>

            <p class="text-xs text-stone-500">Key: <span class="font-mono text-stone-700 select-all">{{ $setupKey }}</span></p>

            <form wire:submit="confirmSetup" class="space-y-3">
                <div>
                    <x-input-label for="authenticator_code" value="Code from your app" />
                    <x-text-input wire:model="code" id="authenticator_code" type="text" inputmode="numeric" maxlength="7" autocomplete="one-time-code" class="mt-1 block w-40 font-mono tracking-widest text-center text-lg" />
                    <x-input-error :messages="$errors->get('code')" class="mt-2" />
                </div>
                <div class="flex items-center gap-4">
                    <x-primary-button>Turn on</x-primary-button>
                    <button type="button" wire:click="cancelSetup" class="text-sm text-stone-500 hover:text-stone-800">Cancel</button>
                </div>
            </form>
        @else
            <x-primary-button wire:click="startSetup">Set up authenticator app</x-primary-button>
        @endif
    </div>
</section>
