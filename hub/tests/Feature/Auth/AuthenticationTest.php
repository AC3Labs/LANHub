<?php

namespace Tests\Feature\Auth;

use App\Mail\TwoFactorCodeMail;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Mail;
use Livewire\Volt\Volt;
use Tests\TestCase;

class AuthenticationTest extends TestCase
{
    use RefreshDatabase;

    public function test_login_screen_can_be_rendered(): void
    {
        $response = $this->get('/login');

        $response
            ->assertOk()
            ->assertSeeVolt('pages.auth.login');
    }

    public function test_correct_credentials_send_a_two_factor_code_instead_of_logging_in_directly(): void
    {
        Mail::fake();

        $user = User::factory()->create();

        $component = Volt::test('pages.auth.login')
            ->set('form.email', $user->email)
            ->set('form.password', 'password');

        $component->call('login');

        $component
            ->assertHasNoErrors()
            ->assertRedirect(route('verify-2fa', absolute: false));

        $this->assertGuest();
        Mail::assertSent(TwoFactorCodeMail::class);
        $this->assertNotNull($user->fresh()->two_factor_code);
    }

    public function test_a_pending_invited_user_cannot_log_in(): void
    {
        $user = User::factory()->create([
            'invite_token' => 'some-token',
            'invite_token_expires_at' => now()->addDays(7),
        ]);

        $component = Volt::test('pages.auth.login')
            ->set('form.email', $user->email)
            ->set('form.password', 'password');

        $component->call('login');

        $component->assertHasErrors();
        $this->assertGuest();
    }

    public function test_the_correct_two_factor_code_completes_login(): void
    {
        $user = User::factory()->create();
        $code = $user->generateTwoFactorCode();
        session(['2fa.user_id' => $user->id]);

        Volt::test('pages.auth.verify-2fa')
            ->set('code', $code)
            ->call('verify')
            ->assertRedirect(route('dashboard', absolute: false));

        $this->assertAuthenticatedAs($user);
        $this->assertNull($user->fresh()->two_factor_code);
    }

    public function test_an_incorrect_two_factor_code_does_not_log_in(): void
    {
        $user = User::factory()->create();
        $user->generateTwoFactorCode();
        session(['2fa.user_id' => $user->id]);

        Volt::test('pages.auth.verify-2fa')
            ->set('code', 'WRONGCODE1')
            ->call('verify')
            ->assertHasErrors('code');

        $this->assertGuest();
    }

    public function test_users_can_not_authenticate_with_invalid_password(): void
    {
        $user = User::factory()->create();

        $component = Volt::test('pages.auth.login')
            ->set('form.email', $user->email)
            ->set('form.password', 'wrong-password');

        $component->call('login');

        $component
            ->assertHasErrors()
            ->assertNoRedirect();

        $this->assertGuest();
    }

    public function test_navigation_menu_can_be_rendered(): void
    {
        $user = User::factory()->create();

        $this->actingAs($user);

        $response = $this->get('/dashboard');

        $response
            ->assertOk()
            ->assertSeeVolt('layout.navigation');
    }

    public function test_users_can_logout(): void
    {
        $user = User::factory()->create();

        $this->actingAs($user);

        $component = Volt::test('layout.navigation');

        $component->call('logout');

        $component
            ->assertHasNoErrors()
            ->assertRedirect('/');

        $this->assertGuest();
    }
}
