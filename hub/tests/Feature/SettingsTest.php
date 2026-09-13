<?php

namespace Tests\Feature;

use App\Livewire\Settings\Index;
use App\Models\Setting;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\DB;
use Livewire\Livewire;
use Tests\TestCase;

class SettingsTest extends TestCase
{
    use RefreshDatabase;

    public function test_non_admins_cannot_access_settings(): void
    {
        $user = User::factory()->create(['is_admin' => false]);

        $this->actingAs($user)->get('/settings')->assertForbidden();
    }

    public function test_an_admin_can_save_smtp_settings(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->set('smtpHost', 'smtp.example.com')
            ->set('smtpPort', 587)
            ->set('smtpUsername', 'someone@example.com')
            ->set('smtpPassword', 'secret-password')
            ->set('smtpFromAddress', 'hello@example.com')
            ->call('save')
            ->assertHasNoErrors();

        $this->assertSame('smtp.example.com', Setting::get('smtp_host'));
        $this->assertSame('secret-password', Setting::get('smtp_password'));
    }

    public function test_leaving_the_password_blank_on_save_keeps_the_existing_one(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        Setting::set('smtp_password', 'original-password');

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->set('smtpHost', 'smtp.example.com')
            ->set('smtpPassword', '')
            ->call('save');

        $this->assertSame('original-password', Setting::get('smtp_password'));
    }

    public function test_the_stored_password_is_encrypted_at_rest(): void
    {
        Setting::set('smtp_password', 'super-secret');

        $raw = DB::table('settings')->where('key', 'smtp_password')->value('value');

        $this->assertStringNotContainsString('super-secret', $raw);
    }
}
