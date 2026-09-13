<?php

namespace Tests\Feature;

use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Hash;
use Tests\TestCase;

class CreateAdminTest extends TestCase
{
    use RefreshDatabase;

    public function test_it_creates_the_first_admin_on_a_fresh_install(): void
    {
        $this->artisan('lanhub:create-admin')
            ->expectsQuestion('Name', 'First Admin')
            ->expectsQuestion('Email', 'first-admin@example.test')
            ->expectsQuestion('Password (min 8 characters)', 'a-real-password')
            ->assertSuccessful();

        $user = User::where('email', 'first-admin@example.test')->firstOrFail();
        $this->assertTrue($user->is_admin);
        $this->assertTrue(Hash::check('a-real-password', $user->password));
        $this->assertNotNull($user->email_verified_at);
    }

    public function test_it_refuses_to_run_if_any_user_already_exists(): void
    {
        User::factory()->create();

        $this->artisan('lanhub:create-admin')
            ->assertFailed();

        $this->assertSame(1, User::count());
    }
}
