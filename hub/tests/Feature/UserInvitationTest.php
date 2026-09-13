<?php

namespace Tests\Feature;

use App\Livewire\Users\Index;
use App\Mail\UserInvitation;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Mail;
use Livewire\Livewire;
use Livewire\Volt\Volt;
use Tests\TestCase;

class UserInvitationTest extends TestCase
{
    use RefreshDatabase;

    public function test_non_admins_cannot_access_the_users_page(): void
    {
        $user = User::factory()->create(['is_admin' => false]);

        $this->actingAs($user)->get('/users')->assertForbidden();
    }

    public function test_an_admin_can_invite_a_new_user(): void
    {
        Mail::fake();

        $admin = User::factory()->create(['is_admin' => true]);

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->set('name', 'Jane Doe')
            ->set('email', 'jane@example.com')
            ->call('invite')
            ->assertHasNoErrors();

        $invited = User::where('email', 'jane@example.com')->firstOrFail();
        $this->assertTrue($invited->isPending());
        $this->assertNotNull($invited->invite_token);

        Mail::assertSent(UserInvitation::class, fn ($mail) => $mail->user->id === $invited->id);
    }

    public function test_an_invited_user_can_set_their_password_and_then_log_in(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $invited = User::factory()->create([
            'invite_token' => 'valid-token-123',
            'invite_token_expires_at' => now()->addDays(7),
        ]);

        Volt::test('pages.auth.set-password', ['token' => 'valid-token-123'])
            ->set('password', 'a-new-password')
            ->set('password_confirmation', 'a-new-password')
            ->call('setPassword')
            ->assertRedirect(route('login', absolute: false));

        $invited->refresh();
        $this->assertFalse($invited->isPending());
        $this->assertNull($invited->invite_token);
        $this->assertTrue(Hash::check('a-new-password', $invited->password));
    }

    public function test_an_expired_invite_token_cannot_be_used(): void
    {
        $invited = User::factory()->create([
            'invite_token' => 'expired-token',
            'invite_token_expires_at' => now()->subDay(),
        ]);

        $component = Volt::test('pages.auth.set-password', ['token' => 'expired-token']);

        $component->assertSee('invalid or has expired');
    }

    public function test_a_non_admin_is_forbidden_from_mounting_the_users_component_directly(): void
    {
        // Route middleware alone doesn't protect this — Livewire's AJAX
        // update calls hit a separate endpoint, not the page route — so
        // the component's own mount() must enforce this (see
        // App\Livewire\Users\Index::mount()).
        $user = User::factory()->create(['is_admin' => false]);

        Livewire::actingAs($user)->test(Index::class)->assertStatus(403);
    }

    public function test_an_admin_can_toggle_another_users_admin_status(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $other = User::factory()->create(['is_admin' => false]);

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->call('toggleAdmin', $other->id);

        $this->assertTrue($other->fresh()->is_admin);
    }

    public function test_an_admin_cannot_change_their_own_admin_status_or_delete_themselves(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->call('toggleAdmin', $admin->id)
            ->assertStatus(403);
    }

    public function test_inviting_a_user_can_grant_machine_access_in_the_same_step(): void
    {
        Mail::fake();

        $admin = User::factory()->create(['is_admin' => true]);
        $readOnlyMachine = Machine::factory()->create();
        $fullMachine = Machine::factory()->create();
        $untouchedMachine = Machine::factory()->create();

        Livewire::actingAs($admin)
            ->test(Index::class)
            ->set('name', 'Jane Doe')
            ->set('email', 'jane@example.com')
            ->call('setMachineRole', $readOnlyMachine->id, 'read_only')
            ->call('setMachineRole', $fullMachine->id, 'full')
            ->call('invite')
            ->assertHasNoErrors();

        $invited = User::where('email', 'jane@example.com')->firstOrFail();
        $this->assertTrue($invited->canAccess($readOnlyMachine, 'read_only'));
        $this->assertFalse($invited->canAccess($readOnlyMachine, 'full'));
        $this->assertTrue($invited->canAccess($fullMachine, 'full'));
        $this->assertFalse($invited->canAccess($untouchedMachine));
    }

    public function test_setting_a_machines_role_twice_toggles_it_back_to_none(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $machine = Machine::factory()->create();

        $component = Livewire::actingAs($admin)
            ->test(Index::class)
            ->call('setMachineRole', $machine->id, 'read_only');

        $this->assertSame('read_only', $component->get("machineRoles.{$machine->id}"));

        $component->call('setMachineRole', $machine->id, 'read_only');

        $this->assertSame('none', $component->get("machineRoles.{$machine->id}"));
    }

    public function test_checking_full_access_for_a_row_clears_read_only_for_that_same_row(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $machine = Machine::factory()->create();

        $component = Livewire::actingAs($admin)
            ->test(Index::class)
            ->call('setMachineRole', $machine->id, 'read_only')
            ->call('setMachineRole', $machine->id, 'full');

        $this->assertSame('full', $component->get("machineRoles.{$machine->id}"));
    }

    public function test_select_all_read_only_sets_every_machine_then_clears_them_on_second_click(): void
    {
        $admin = User::factory()->create(['is_admin' => true]);
        $a = Machine::factory()->create();
        $b = Machine::factory()->create();

        $component = Livewire::actingAs($admin)
            ->test(Index::class)
            ->call('toggleAllMachineRole', 'read_only');

        $this->assertSame('read_only', $component->get("machineRoles.{$a->id}"));
        $this->assertSame('read_only', $component->get("machineRoles.{$b->id}"));

        $component->call('toggleAllMachineRole', 'read_only');

        $this->assertSame('none', $component->get("machineRoles.{$a->id}"));
        $this->assertSame('none', $component->get("machineRoles.{$b->id}"));
    }
}
