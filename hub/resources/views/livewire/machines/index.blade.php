<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Machines</h2>
                <p class="text-sm text-stone-500 mt-1">Every computer LANHub can reach on your network.</p>
            </div>
            <button wire:click="openCreate" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"/></svg>
                Add Machine
            </button>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($machines->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">No machines registered yet.</p>
                <button wire:click="openCreate" class="mt-4 inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg transition">
                    Add your first machine
                </button>
            </div>
        @else
            <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">
                @foreach ($machines as $machine)
                    @php $status = $statuses[$machine->id]['status'] ?? $machine->status; @endphp
                    <div class="rounded-xl bg-paper shadow-soft border border-stone-200 p-5 flex flex-col gap-4">
                        <div class="flex items-start justify-between">
                            <div class="flex items-center gap-3">
                                <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-white font-semibold" style="background-color: {{ $machine->color }}">
                                    {{ strtoupper(substr($machine->name, 0, 1)) }}
                                </span>
                                <div>
                                    <p class="font-medium text-stone-800">{{ $machine->name }}</p>
                                    <p class="text-xs text-stone-500">{{ $machine->host }}:{{ $machine->port }}</p>
                                </div>
                            </div>
                            <span @class([
                                'inline-flex items-center gap-1.5 text-xs font-medium px-2 py-1 rounded-full',
                                'bg-green-100 text-green-700' => $status === 'online',
                                'bg-red-100 text-red-700' => $status === 'offline',
                                'bg-stone-100 text-stone-500' => ! in_array($status, ['online', 'offline']),
                            ])>
                                <span @class([
                                    'w-1.5 h-1.5 rounded-full',
                                    'bg-green-500' => $status === 'online',
                                    'bg-red-500' => $status === 'offline',
                                    'bg-stone-400' => ! in_array($status, ['online', 'offline']),
                                ])></span>
                                {{ ucfirst($status) }}
                            </span>
                        </div>

                        <div class="flex items-center gap-2 text-xs text-stone-500">
                            <span class="capitalize px-2 py-0.5 rounded bg-stone-100">{{ $machine->os }}</span>
                            @if ($machine->last_seen_at)
                                <span>Last seen {{ $machine->last_seen_at->diffForHumans() }}</span>
                            @endif
                        </div>

                        @if ($machine->notes)
                            <p class="text-sm text-stone-500 line-clamp-2">{{ $machine->notes }}</p>
                        @endif

                        <div class="flex items-center gap-2 pt-2 border-t border-stone-100 mt-auto">
                            <button wire:click="checkStatus({{ $machine->id }})" wire:loading.attr="disabled" wire:target="checkStatus({{ $machine->id }})" class="flex-1 text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                <span wire:loading.remove wire:target="checkStatus({{ $machine->id }})">Ping</span>
                                <span wire:loading wire:target="checkStatus({{ $machine->id }})">Checking…</span>
                            </button>
                            <a href="{{ route('explorer') }}?machine={{ $machine->id }}" wire:navigate class="flex-1 text-center text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                Browse
                            </a>
                            <button wire:click="openEdit({{ $machine->id }})" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                Edit
                            </button>
                            <button wire:click="delete({{ $machine->id }})" wire:confirm="Remove {{ $machine->name }}? This cannot be undone." class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-red-600 hover:bg-red-50 transition">
                                Delete
                            </button>
                        </div>
                    </div>
                @endforeach
            </div>
        @endif
    </div>

    <x-modal name="machine-form" :show="$errors->isNotEmpty()" maxWidth="lg">
        <form wire:submit="save" class="p-6">
            <h3 class="text-lg font-semibold text-stone-800 mb-4">
                {{ $editingId ? 'Edit machine' : 'Add a machine' }}
            </h3>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div class="sm:col-span-2">
                    <x-input-label for="name" value="Name" />
                    <x-text-input id="name" wire:model="name" class="mt-1 block w-full" placeholder="Desktop — Office" />
                    <x-input-error :messages="$errors->get('name')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="host" value="Host / IP" />
                    <x-text-input id="host" wire:model="host" class="mt-1 block w-full" placeholder="192.168.1.42" />
                    <x-input-error :messages="$errors->get('host')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="port" value="Port" />
                    <x-text-input id="port" type="number" wire:model="port" class="mt-1 block w-full" />
                    <x-input-error :messages="$errors->get('port')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="os" value="Operating system" />
                    <select id="os" wire:model="os" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                        <option value="windows">Windows</option>
                        <option value="linux">Linux</option>
                        <option value="macos">macOS</option>
                        <option value="other">Other</option>
                    </select>
                </div>

                <div>
                    <x-input-label for="color" value="Color tag" />
                    <input id="color" type="color" wire:model="color" class="mt-1 block w-full h-10 border-stone-300 rounded-md shadow-sm" />
                </div>

                <div class="sm:col-span-2">
                    <div class="flex items-center justify-between">
                        <x-input-label for="agentToken" :value="$editingId ? 'Agent token (leave blank to keep current)' : 'Agent token'" />
                        <button type="button" wire:click="suggestToken" class="text-xs text-tan-600 hover:text-tan-700">Generate</button>
                    </div>
                    <x-text-input id="agentToken" wire:model="agentToken" class="mt-1 block w-full font-mono text-sm" placeholder="Paste into the agent's config.json" />
                    <x-input-error :messages="$errors->get('agentToken')" class="mt-1" />
                </div>

                <div class="sm:col-span-2 flex items-center gap-2">
                    <input id="useTls" type="checkbox" wire:model="useTls" class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                    <x-input-label for="useTls" value="Use HTTPS to reach the agent" class="!mb-0" />
                </div>

                <div class="sm:col-span-2">
                    <x-input-label for="notes" value="Notes" />
                    <textarea id="notes" wire:model="notes" rows="2" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400"></textarea>
                </div>

                @php $shareableUsers = $this->shareableUsers(); @endphp
                @if ($shareableUsers->isNotEmpty())
                    <div class="sm:col-span-2">
                        <x-input-label value="Shared with" />
                        <p class="text-xs text-stone-500 mt-0.5 mb-2">You always have full access. Grant other users read-only or full access to this machine.</p>
                        <div class="space-y-2">
                            @foreach ($shareableUsers as $user)
                                <div class="flex items-center justify-between gap-3 text-sm">
                                    <span class="text-stone-700">{{ $user->name }}</span>
                                    <select wire:model="shareRoles.{{ $user->id }}" class="border-stone-300 rounded-md shadow-sm text-xs focus:border-tan-400 focus:ring-tan-400">
                                        <option value="none">No access</option>
                                        <option value="read_only">Read-only</option>
                                        <option value="full">Full access</option>
                                    </select>
                                </div>
                            @endforeach
                        </div>
                    </div>
                @endif
            </div>

            <div class="mt-6 flex justify-end gap-3">
                <button type="button" x-on:click="$dispatch('close-modal', 'machine-form')" class="px-4 py-2 text-sm font-medium text-stone-600 hover:text-stone-800">Cancel</button>
                <x-primary-button>{{ $editingId ? 'Save changes' : 'Add machine' }}</x-primary-button>
            </div>
        </form>
    </x-modal>
</div>
