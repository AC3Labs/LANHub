<?php

namespace Tests\Feature;

use App\Mail\TwoFactorCodeMail;
use App\Models\Setting;
use App\Models\User;
use App\Support\Authenticator;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Mail;
use Livewire\Volt\Volt;
use PragmaRX\Google2FA\Google2FA;
use Tests\TestCase;

class AuthenticatorAppTest extends TestCase
{
    use RefreshDatabase;

    private function currentCode(string $secret): string
    {
        return (new Google2FA)->getCurrentOtp($secret);
    }

    private function userWithAuthenticator(): User
    {
        $user = User::factory()->create();
        $user->forceFill([
            'two_factor_secret' => Authenticator::newSecret(),
            'two_factor_confirmed_at' => now(),
            'two_factor_recovery_codes' => ['AAAAA-BBBBB', 'CCCCC-DDDDD'],
        ])->save();

        return $user->fresh();
    }

    public function test_a_user_can_set_up_an_authenticator_app_from_their_profile(): void
    {
        $user = User::factory()->create();
        $this->actingAs($user);

        $component = Volt::test('profile.two-factor-authentication-form')
            ->call('startSetup')
            ->assertSee('Scan this QR code');

        $secret = $user->fresh()->two_factor_secret;
        $this->assertNotNull($secret);
        $this->assertFalse($user->fresh()->hasAuthenticator(), 'not active until confirmed');

        $component->set('code', '000000')->call('confirmSetup')->assertHasErrors('code');
        $this->assertFalse($user->fresh()->hasAuthenticator());

        $component->set('code', $this->currentCode($secret))
            ->call('confirmSetup')
            ->assertHasNoErrors()
            ->assertSee('Save these recovery codes');

        $user->refresh();
        $this->assertTrue($user->hasAuthenticator());
        $this->assertCount(8, $user->two_factor_recovery_codes);
    }

    public function test_the_secret_and_recovery_codes_are_encrypted_at_rest(): void
    {
        $user = $this->userWithAuthenticator();
        $raw = \DB::table('users')->where('id', $user->id)->first();

        $this->assertNotSame($user->two_factor_secret, $raw->two_factor_secret);
        $this->assertStringNotContainsString('AAAAA-BBBBB', $raw->two_factor_recovery_codes);
    }

    public function test_login_asks_for_the_app_code_instead_of_emailing_one(): void
    {
        Mail::fake();
        $user = $this->userWithAuthenticator();

        Volt::test('pages.auth.login')
            ->set('form.email', $user->email)
            ->set('form.password', 'password')
            ->call('login')
            ->assertRedirect(route('verify-2fa', absolute: false));

        Mail::assertNothingSent();
        $this->assertSame('app', session('2fa.method'));

        Volt::test('pages.auth.verify-2fa')
            ->assertSee('authenticator app')
            ->set('code', $this->currentCode($user->two_factor_secret))
            ->call('verify')
            ->assertRedirect(route('dashboard', absolute: false));

        $this->assertAuthenticatedAs($user);
    }

    public function test_an_app_code_cannot_be_used_twice(): void
    {
        $user = $this->userWithAuthenticator();
        $code = $this->currentCode($user->two_factor_secret);

        $this->assertTrue($user->verifyAuthenticatorCode($code));
        $this->assertFalse($user->fresh()->verifyAuthenticatorCode($code));
    }

    public function test_a_wrong_app_code_does_not_log_in(): void
    {
        $user = $this->userWithAuthenticator();
        session(['2fa.user_id' => $user->id, '2fa.method' => 'app']);

        Volt::test('pages.auth.verify-2fa')
            ->set('code', '123456')
            ->call('verify')
            ->assertHasErrors('code');

        $this->assertGuest();
    }

    public function test_a_recovery_code_works_once(): void
    {
        $user = $this->userWithAuthenticator();
        session(['2fa.user_id' => $user->id, '2fa.method' => 'app']);

        Volt::test('pages.auth.verify-2fa')
            ->set('code', 'aaaaa bbbbb')
            ->call('verify')
            ->assertRedirect(route('dashboard', absolute: false));

        $this->assertSame(['CCCCC-DDDDD'], $user->fresh()->two_factor_recovery_codes);
        $this->assertFalse($user->fresh()->verifyAuthenticatorCode('AAAAA-BBBBB'));
    }

    public function test_an_app_user_can_ask_for_an_emailed_code_instead_when_email_is_set_up(): void
    {
        Mail::fake();
        Setting::set('smtp_host', 'smtp.example.com');
        $user = $this->userWithAuthenticator();
        session(['2fa.user_id' => $user->id, '2fa.method' => 'app']);

        $component = Volt::test('pages.auth.verify-2fa')
            ->assertSee('Email me a code instead')
            ->call('resend')
            ->assertSet('method', 'email');

        Mail::assertSent(TwoFactorCodeMail::class);
        $this->assertSame('email', session('2fa.method'));
    }

    public function test_the_email_option_is_hidden_when_email_is_not_set_up(): void
    {
        $user = $this->userWithAuthenticator();
        session(['2fa.user_id' => $user->id, '2fa.method' => 'app']);

        Volt::test('pages.auth.verify-2fa')->assertDontSee('Email me a code instead');
    }

    public function test_turning_it_off_requires_the_password(): void
    {
        $user = $this->userWithAuthenticator();
        $this->actingAs($user);

        Volt::test('profile.two-factor-authentication-form')
            ->set('current_password', 'wrong-password')
            ->call('remove')
            ->assertHasErrors('current_password');
        $this->assertTrue($user->fresh()->hasAuthenticator());

        Volt::test('profile.two-factor-authentication-form')
            ->set('current_password', 'password')
            ->call('remove')
            ->assertHasNoErrors();
        $this->assertFalse($user->fresh()->hasAuthenticator());
        $this->assertNull($user->fresh()->two_factor_secret);
    }

    public function test_a_lockout_warning_shows_when_there_is_no_way_to_get_a_login_code(): void
    {
        $user = User::factory()->create();

        $this->actingAs($user)->get('/profile')->assertSee('Before you log out');

        Setting::set('smtp_host', 'smtp.example.com');
        $this->actingAs($user)->get('/profile')->assertDontSee('Before you log out');
    }

    public function test_the_lockout_warning_is_gone_once_an_authenticator_is_set_up(): void
    {
        $this->actingAs($this->userWithAuthenticator())->get('/profile')->assertDontSee('Before you log out');
    }

    public function test_the_reset_command_removes_a_lost_authenticator(): void
    {
        $user = $this->userWithAuthenticator();

        $this->artisan('lanhub:reset-authenticator', ['email' => $user->email])->assertSuccessful();

        $this->assertFalse($user->fresh()->hasAuthenticator());
    }
}
