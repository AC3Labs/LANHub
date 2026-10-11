<?php

namespace Tests\Feature;

use App\Livewire\Settings\Index as SettingsIndex;
use App\Livewire\Setup\Index as SetupIndex;
use App\Mail\UserInvitation;
use App\Models\User;
use App\Notifications\ResetPasswordNotification;
use App\Providers\AppServiceProvider;
use App\Support\PublicUrl;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Notification;
use Livewire\Livewire;
use Livewire\Volt\Volt;
use Tests\TestCase;

/**
 * A fresh install has to work at whatever address it's reached on, with no
 * APP_URL edited — while email links stay pinned to a trusted address.
 */
class PublicUrlTest extends TestCase
{
    use RefreshDatabase;

    private function useAppUrl(string $url): void
    {
        config(['app.url' => $url]);
        (new AppServiceProvider($this->app))->boot();
    }

    public function test_the_compose_placeholder_and_laravel_default_are_not_treated_as_a_real_address(): void
    {
        foreach (['http://localhost', 'http://localhost:8000', 'http://127.0.0.1:8949', ''] as $url) {
            config(['app.url' => $url]);
            $this->assertNull(PublicUrl::configured(), $url);
        }

        config(['app.url' => 'http://192.168.1.10:8949/']);
        $this->assertSame('http://192.168.1.10:8949', PublicUrl::configured());
    }

    public function test_pages_link_to_the_address_the_browser_actually_used(): void
    {
        $this->get('http://192.168.1.10:8949/')
            ->assertRedirect('http://192.168.1.10:8949/setup');
    }

    public function test_pages_behind_an_https_reverse_proxy_link_to_the_proxys_domain(): void
    {
        $this->withHeaders([
            'X-Forwarded-Proto' => 'https',
            'X-Forwarded-Host' => 'lanhub.example.com',
            'X-Forwarded-Port' => '443',
        ])->get('http://10.0.1.5:8000/')
            ->assertRedirect('https://lanhub.example.com/setup');
    }

    public function test_a_real_app_url_still_overrides_the_request_address(): void
    {
        $this->useAppUrl('https://hub.example.com');

        $this->get('http://192.168.1.10:8949/')
            ->assertRedirect('https://hub.example.com/setup');
    }

    public function test_the_setup_wizard_records_the_address_the_admin_used(): void
    {
        $this->assertNull(PublicUrl::stored());

        Livewire::test(SetupIndex::class)
            ->set('name', 'First Admin')
            ->set('email', 'first-admin@example.test')
            ->set('password', 'a-real-password')
            ->set('password_confirmation', 'a-real-password')
            ->call('create')
            ->assertHasNoErrors();

        $this->assertSame(request()->getSchemeAndHttpHost(), PublicUrl::stored());
    }

    public function test_remembering_never_overwrites_a_known_address(): void
    {
        PublicUrl::rememberFrom(Request::create('http://192.168.1.10:8949/settings'));
        $this->assertSame('http://192.168.1.10:8949', PublicUrl::stored());

        PublicUrl::rememberFrom(Request::create('http://evil.example/settings'));
        $this->assertSame('http://192.168.1.10:8949', PublicUrl::stored());
    }

    public function test_an_admin_login_records_the_address_on_installs_that_predate_this(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $code = $admin->generateTwoFactorCode();
        session(['2fa.user_id' => $admin->id]);

        Volt::test('pages.auth.verify-2fa')->set('code', $code)->call('verify');

        $this->assertNotNull(PublicUrl::stored());
    }

    public function test_a_non_admin_login_does_not_record_an_address(): void
    {
        $user = User::factory()->create();
        $code = $user->generateTwoFactorCode();
        session(['2fa.user_id' => $user->id]);

        Volt::test('pages.auth.verify-2fa')->set('code', $code)->call('verify');

        $this->assertNull(PublicUrl::stored());
    }

    public function test_password_reset_links_ignore_a_forged_host_header(): void
    {
        Notification::fake();
        PublicUrl::store('http://192.168.1.10:8949');
        $user = User::factory()->create();

        $this->withHeaders(['Host' => 'evil.example'])->get('/forgot-password');
        Volt::test('pages.auth.forgot-password')
            ->set('email', $user->email)
            ->call('sendPasswordResetLink');

        Notification::assertSentTo($user, ResetPasswordNotification::class, function ($notification) use ($user) {
            $html = $notification->toMail($user)->render();

            return str_contains($html, 'http://192.168.1.10:8949/reset-password/'.$notification->token)
                && ! str_contains($html, 'evil.example');
        });
    }

    public function test_invite_links_use_the_stored_address(): void
    {
        PublicUrl::store('https://lanhub.example.com');
        $user = User::factory()->create(['invite_token' => 'abc123']);

        $html = (new UserInvitation($user))->render();

        $this->assertStringContainsString('https://lanhub.example.com/set-password/abc123', $html);
    }

    public function test_an_admin_can_change_the_hub_address_in_settings(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);

        Livewire::actingAs($admin)
            ->test(SettingsIndex::class)
            ->set('hubAddress', 'not a url')
            ->call('saveHubAddress')
            ->assertHasErrors('hubAddress')
            ->set('hubAddress', 'https://lanhub.example.com/')
            ->call('saveHubAddress')
            ->assertHasNoErrors();

        $this->assertSame('https://lanhub.example.com', PublicUrl::stored());
    }

    public function test_settings_shows_app_url_instead_of_the_field_when_it_is_set(): void
    {
        $this->useAppUrl('https://hub.example.com');
        $admin = User::factory()->create(['is_admin' => true]);

        Livewire::actingAs($admin)
            ->test(SettingsIndex::class)
            ->assertSet('hubAddressFromEnv', 'https://hub.example.com')
            ->assertDontSee('Use the address I&#039;m on now', false);
    }
}
