<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Users</h2>
                <p class="text-sm text-stone-500 mt-1">Everyone with access to LANHub, and what they can see.</p>
            </div>
            <button wire:click="openCreate" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition">
                + Invite user
            </button>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        <div class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden divide-y divide-stone-100">
            @foreach ($users as $user)
                <div class="p-4 flex items-center justify-between gap-4 flex-wrap">
                    <div class="min-w-0">
                        <div class="flex items-center gap-2">
                            <p class="font-medium text-stone-800">{{ $user->name }}</p>
                            @if ($user->is_admin)
                                <span class="text-xs font-medium px-2 py-0.5 rounded-full bg-tan-100 text-tan-800">Admin</span>
                            @endif
                            @if ($user->isPending())
                                <span class="text-xs font-medium px-2 py-0.5 rounded-full bg-amber-100 text-amber-700">Invited — not activated</span>
                            @endif
                        </div>
                        <p class="text-sm text-stone-500">{{ $user->email }}</p>
                    </div>
                    <div class="flex items-center gap-2 shrink-0">
                        @if ($user->isPending())
                            <button wire:click="resendInvite({{ $user->id }})" wire:confirm="Resend the invite email to {{ $user->email }}?" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                Resend invite
                            </button>
                        @endif
                        @if ($user->id !== auth()->id())
                            <button wire:click="toggleAdmin({{ $user->id }})" wire:confirm="{{ $user->is_admin ? 'Remove admin access from' : 'Grant admin access to' }} {{ $user->name }}?" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                {{ $user->is_admin ? 'Remove admin' : 'Make admin' }}
                            </button>
                            <button wire:click="delete({{ $user->id }})" wire:confirm="Remove {{ $user->name }}? This cannot be undone." class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-red-600 hover:bg-red-50 transition">
                                Delete
                            </button>
                        @else
                            <span class="text-xs text-stone-400 italic px-3 py-1.5">You</span>
                        @endif
                    </div>
                </div>
            @endforeach
        </div>
    </div>

    <x-modal name="invite-user" :show="$errors->isNotEmpty()" maxWidth="md">
        <form wire:submit="invite" class="p-6">
            <h3 class="text-lg font-semibold text-stone-800 mb-4">Invite a user</h3>

            <div class="space-y-4">
                <div>
                    <x-input-label for="invite-name" value="Name" />
                    <x-text-input id="invite-name" wire:model="name" class="mt-1 block w-full" placeholder="Jane Doe" />
                    <x-input-error :messages="$errors->get('name')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="invite-email" value="Email" />
                    <x-text-input id="invite-email" wire:model="email" type="email" class="mt-1 block w-full" placeholder="jane@example.com" />
                    <x-input-error :messages="$errors->get('email')" class="mt-1" />
                </div>

                <p class="text-xs text-stone-500">They'll get an email with a link to set their own password.</p>

                @if ($machines->isNotEmpty())
                    <div>
                        <x-input-label value="Machine access" />
                        <div class="mt-1 rounded-lg border border-stone-200 overflow-hidden">
                            <table class="w-full text-sm">
                                <thead class="bg-stone-50 text-stone-500">
                                    <tr>
                                        <th class="text-left font-medium px-3 py-2">Machine</th>
                                        <th class="text-center font-medium px-3 py-2 w-24">
                                            <label class="inline-flex items-center gap-1.5 cursor-pointer">
                                                <input type="checkbox" wire:click="toggleAllMachineRole('read_only')" @checked($this->allMachinesHaveRole('read_only')) class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                                                Read-only
                                            </label>
                                        </th>
                                        <th class="text-center font-medium px-3 py-2 w-24">
                                            <label class="inline-flex items-center gap-1.5 cursor-pointer">
                                                <input type="checkbox" wire:click="toggleAllMachineRole('full')" @checked($this->allMachinesHaveRole('full')) class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                                                Full
                                            </label>
                                        </th>
                                    </tr>
                                </thead>
                                <tbody class="divide-y divide-stone-100">
                                    @foreach ($machines as $machine)
                                        <tr wire:key="invite-machine-{{ $machine->id }}">
                                            <td class="px-3 py-2">
                                                <span class="flex items-center gap-1.5">
                                                    <span class="w-2 h-2 rounded-full shrink-0" style="background-color: {{ $machine->color }}"></span>
                                                    {{ $machine->name }}
                                                </span>
                                            </td>
                                            <td class="text-center px-3 py-2">
                                                <input type="checkbox" wire:click="setMachineRole({{ $machine->id }}, 'read_only')" @checked(($machineRoles[$machine->id] ?? 'none') === 'read_only') class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                                            </td>
                                            <td class="text-center px-3 py-2">
                                                <input type="checkbox" wire:click="setMachineRole({{ $machine->id }}, 'full')" @checked(($machineRoles[$machine->id] ?? 'none') === 'full') class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                                            </td>
                                        </tr>
                                    @endforeach
                                </tbody>
                            </table>
                        </div>
                    </div>
                @endif
            </div>

            <div class="mt-6 flex justify-end gap-3">
                <button type="button" x-on:click="$dispatch('close-modal', 'invite-user')" class="px-4 py-2 text-sm font-medium text-stone-600 hover:text-stone-800">Cancel</button>
                <x-primary-button>Send invite</x-primary-button>
            </div>
        </form>
    </x-modal>
</div>
