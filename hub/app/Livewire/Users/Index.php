<?php

namespace App\Livewire\Users;

use App\Mail\UserInvitation;
use App\Models\Machine;
use App\Models\User;
use Illuminate\Support\Collection;
use Illuminate\Support\Facades\Auth;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Mail;
use Illuminate\Support\Str;
use Livewire\Attributes\Layout;
use Livewire\Attributes\Validate;
use Livewire\Component;

#[Layout('layouts.app')]
class Index extends Component
{
    #[Validate('required|string|max:255')]
    public string $name = '';

    #[Validate('required|string|email|max:255|unique:users,email')]
    public string $email = '';

    /** @var array<int, 'none'|'read_only'|'full'> keyed by machine id */
    public array $machineRoles = [];

    public function mount(): void
    {
        // The 'admin' middleware on this component's own route already
        // gates the initial page load, but Livewire's subsequent AJAX
        // calls (invite/resendInvite/toggleAdmin/delete) hit a separate
        // update endpoint that doesn't re-check route middleware — this
        // is the actual gate those go through.
        abort_unless(Auth::user()->is_admin, 403);

        $this->resetMachineRoles();
    }

    public function openCreate(): void
    {
        $this->reset(['name', 'email']);
        $this->resetMachineRoles();
        $this->resetErrorBag();
        $this->dispatch('open-modal', 'invite-user');
    }

    /**
     * Toggles a single machine's role for a single checkbox click — since
     * a machine's access is one role (none/read-only/full), checking one
     * box for a row implicitly clears the other one for that same row.
     */
    public function setMachineRole(int $machineId, string $role): void
    {
        $current = $this->machineRoles[$machineId] ?? 'none';
        $this->machineRoles[$machineId] = $current === $role ? 'none' : $role;
    }

    /**
     * The "select all" checkbox above a column — sets every machine to
     * that role if not all of them already are, otherwise clears just
     * the ones currently on that role (leaving the other column alone).
     */
    public function toggleAllMachineRole(string $role): void
    {
        $machineIds = $this->availableMachines()->pluck('id');
        $allSet = $machineIds->every(fn ($id) => ($this->machineRoles[$id] ?? 'none') === $role);

        foreach ($machineIds as $id) {
            if ($allSet) {
                if (($this->machineRoles[$id] ?? 'none') === $role) {
                    $this->machineRoles[$id] = 'none';
                }
            } else {
                $this->machineRoles[$id] = $role;
            }
        }
    }

    public function allMachinesHaveRole(string $role): bool
    {
        $machineIds = $this->availableMachines()->pluck('id');

        return $machineIds->isNotEmpty() && $machineIds->every(fn ($id) => ($this->machineRoles[$id] ?? 'none') === $role);
    }

    private function resetMachineRoles(): void
    {
        $this->machineRoles = $this->availableMachines()->mapWithKeys(fn (Machine $m) => [$m->id => 'none'])->all();
    }

    /** @return Collection<int, Machine> */
    public function availableMachines()
    {
        return Machine::orderBy('name')->get();
    }

    public function invite(): void
    {
        $this->validate();

        $user = User::create([
            'name' => $this->name,
            'email' => $this->email,
            // Unusable until they follow the set-password link — nobody
            // can log in with this, it's just satisfying the not-null
            // password column.
            'password' => Hash::make(Str::random(40)),
        ]);

        $sync = [];
        foreach ($this->machineRoles as $machineId => $role) {
            if ($role !== 'none') {
                $sync[$machineId] = ['role' => $role];
            }
        }
        $user->machines()->sync($sync);

        $this->sendInvite($user);

        $this->reset(['name', 'email']);
        $this->resetMachineRoles();
        $this->resetErrorBag();
        $this->dispatch('close-modal', 'invite-user');
    }

    public function resendInvite(int $userId): void
    {
        $user = User::findOrFail($userId);

        if (! $user->isPending()) {
            return;
        }

        $this->sendInvite($user);
    }

    public function toggleAdmin(int $userId): void
    {
        $user = User::findOrFail($userId);

        abort_if($user->id === Auth::id(), 403, "You can't change your own admin status.");

        // update() goes through fill(), which is limited to User's
        // #[Fillable(['name','email','password'])] — is_admin isn't in
        // it, so update() here would silently do nothing.
        $user->forceFill(['is_admin' => ! $user->is_admin])->save();
    }

    public function delete(int $userId): void
    {
        abort_if($userId === Auth::id(), 403, "You can't delete your own account.");

        User::findOrFail($userId)->delete();
    }

    private function sendInvite(User $user): void
    {
        $user->forceFill([
            'invite_token' => Str::random(64),
            'invite_token_expires_at' => now()->addDays(7),
        ])->save();

        Mail::to($user->email)->send(new UserInvitation($user));
    }

    public function render()
    {
        return view('livewire.users.index', [
            'users' => User::orderBy('name')->get(),
            'machines' => $this->availableMachines(),
        ]);
    }
}
