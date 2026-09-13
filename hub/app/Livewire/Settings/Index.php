<?php

namespace App\Livewire\Settings;

use App\Mail\TwoFactorCodeMail;
use App\Models\Setting;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Mail;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    #[Validate('nullable|string|max:255')]
    public string $smtpHost = '';

    #[Validate('nullable|integer|min:1|max:65535')]
    public ?int $smtpPort = 587;

    #[Validate('nullable|string|max:255')]
    public string $smtpUsername = '';

    // Blank on load (never displayed back) and left blank on save means
    // "keep the current password" — same pattern as Machine.agent_token.
    #[Validate('nullable|string|max:255')]
    public string $smtpPassword = '';

    #[Validate('required|in:tls,ssl,none')]
    public string $smtpEncryption = 'tls';

    #[Validate('nullable|email|max:255')]
    public string $smtpFromAddress = '';

    #[Validate('nullable|string|max:255')]
    public string $smtpFromName = '';

    public ?string $testEmailStatus = null;

    public function mount(): void
    {
        abort_unless(Auth::user()->is_admin, 403);

        $this->smtpHost = Setting::get('smtp_host', '') ?? '';
        $this->smtpPort = (int) (Setting::get('smtp_port', '587') ?? 587);
        $this->smtpUsername = Setting::get('smtp_username', '') ?? '';
        $this->smtpEncryption = Setting::get('smtp_encryption', 'tls') ?: 'none';
        $this->smtpFromAddress = Setting::get('smtp_from_address', '') ?? '';
        $this->smtpFromName = Setting::get('smtp_from_name', config('app.name')) ?? '';
    }

    public function save(): void
    {
        abort_unless(Auth::user()->is_admin, 403);
        $this->validate();

        Setting::set('smtp_host', $this->smtpHost ?: null);
        Setting::set('smtp_port', (string) $this->smtpPort);
        Setting::set('smtp_username', $this->smtpUsername ?: null);
        Setting::set('smtp_encryption', $this->smtpEncryption === 'none' ? '' : $this->smtpEncryption);
        Setting::set('smtp_from_address', $this->smtpFromAddress ?: null);
        Setting::set('smtp_from_name', $this->smtpFromName ?: null);

        if ($this->smtpPassword !== '') {
            Setting::set('smtp_password', $this->smtpPassword);
            $this->smtpPassword = '';
        }

        $this->applyToRuntimeConfig();

        session()->flash('status', 'Settings saved.');
    }

    public function sendTestEmail(): void
    {
        abort_unless(Auth::user()->is_admin, 403);
        $this->applyToRuntimeConfig();

        try {
            Mail::to(Auth::user()->email)->send(new TwoFactorCodeMail('TESTCODE1'));
            $this->testEmailStatus = 'Test email sent to '.Auth::user()->email.' — check your inbox.';
        } catch (\Throwable $e) {
            $this->testEmailStatus = 'Failed to send: '.$e->getMessage();
        }
    }

    private function applyToRuntimeConfig(): void
    {
        if (! Setting::get('smtp_host')) {
            return;
        }

        config([
            'mail.default' => 'smtp',
            'mail.mailers.smtp.host' => Setting::get('smtp_host'),
            'mail.mailers.smtp.port' => Setting::get('smtp_port', '587'),
            'mail.mailers.smtp.username' => Setting::get('smtp_username'),
            'mail.mailers.smtp.password' => Setting::get('smtp_password'),
            'mail.mailers.smtp.scheme' => Setting::get('smtp_encryption') === 'ssl' ? 'smtps' : 'smtp',
            'mail.from.address' => Setting::get('smtp_from_address', config('mail.from.address')),
            'mail.from.name' => Setting::get('smtp_from_name', config('mail.from.name')),
        ]);
    }

    public function render()
    {
        return view('livewire.settings.index');
    }
}
