<?php

namespace Tests\Feature;

use App\Livewire\Setup\Index;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Hash;
use Livewire\Livewire;
use Tests\TestCase;

class SetupWizardTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_creates_the_first_admin_and_logs_them_straight_in(): void
    {
        Livewire::test(Index::class)
            ->set('name', 'First Admin')
            ->set('email', 'first-admin@example.test')
            ->set('password', 'a-real-password')
            ->set('password_confirmation', 'a-real-password')
            ->call('create')
            ->assertHasNoErrors()
            ->assertRedirect(route('settings'));

        $user = User::where('email', 'first-admin@example.test')->firstOrFail();
        $this->assertTrue($user->is_admin);
        $this->assertTrue(Hash::check('a-real-password', $user->password));
        $this->assertNotNull($user->email_verified_at);
        $this->assertTrue(Auth::check());
        $this->assertSame($user->id, Auth::id());
    }

    public function test_it_is_unreachable_once_any_user_already_exists(): void
    {
        User::factory()->create();

        Livewire::test(Index::class)
            ->assertRedirect(route('login'));
    }

    public function test_root_redirects_to_setup_on_a_fresh_install(): void
    {
        $this->get('/')->assertRedirect(route('setup'));
    }

    public function test_login_page_redirects_to_setup_on_a_fresh_install(): void
    {
        $this->get('/login')->assertRedirect(route('setup'));
    }

    public function test_setup_redirects_to_login_once_a_user_exists(): void
    {
        User::factory()->create();

        $this->get('/setup')->assertRedirect(route('login'));
    }
}
