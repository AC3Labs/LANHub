<div>
    <div class="bg-paper border-b border-stone-200">
        <div class="max-w-[1600px] mx-auto py-6 px-4 sm:px-6 lg:px-8 flex items-center justify-between">
            <div>
                <h2 class="font-semibold text-xl text-stone-800">Sync Rules</h2>
                <p class="text-sm text-stone-500 mt-1">Automatically keep a folder on one machine in sync with a folder on another.</p>
            </div>
            <button wire:click="openCreate" class="inline-flex items-center gap-2 px-4 py-2 bg-tan-500 hover:bg-tan-600 text-white text-sm font-medium rounded-lg shadow-soft transition">
                + New rule
            </button>
        </div>
    </div>

    <div class="max-w-[1600px] mx-auto py-8 px-4 sm:px-6 lg:px-8">
        @if ($rules->isEmpty())
            <div class="rounded-xl border border-dashed border-stone-300 bg-paper p-12 text-center">
                <p class="text-stone-500">No sync rules yet.</p>
            </div>
        @else
            <div class="rounded-xl bg-paper shadow-soft border border-stone-200 overflow-hidden divide-y divide-stone-100">
                @foreach ($rules as $rule)
                    <div class="p-4 flex items-center justify-between gap-4 flex-wrap">
                        <div class="min-w-0">
                            <div class="flex items-center gap-2">
                                <p class="font-medium text-stone-800">{{ $rule->name }}</p>
                                @if ($rule->has_conflict)
                                    <span class="text-xs font-medium px-2 py-0.5 rounded-full bg-amber-100 text-amber-700">Conflict — some files skipped</span>
                                @endif
                                @if ($rule->last_error)
                                    <span class="text-xs font-medium px-2 py-0.5 rounded-full bg-red-100 text-red-700" title="{{ $rule->last_error }}">Error</span>
                                @endif
                                @unless ($rule->enabled)
                                    <span class="text-xs font-medium px-2 py-0.5 rounded-full bg-stone-100 text-stone-500">Paused</span>
                                @endunless
                            </div>
                            <p class="text-xs text-stone-500 mt-1 font-mono truncate flex items-center gap-1.5">
                                <span class="w-1.5 h-1.5 rounded-full shrink-0" style="background-color: {{ $rule->sourceMachine->color }}"></span>
                                {{ $rule->sourceMachine->name }}:{{ $rule->source_path }}
                                <span class="text-stone-300">{{ $rule->direction === 'mirror' ? '⇄' : '→' }}</span>
                                <span class="w-1.5 h-1.5 rounded-full shrink-0" style="background-color: {{ $rule->destinationMachine->color }}"></span>
                                {{ $rule->destinationMachine->name }}:{{ $rule->destination_path }}
                            </p>
                            <p class="text-xs text-stone-400 mt-0.5">
                                Every {{ $rule->interval_minutes }} min · {{ $rule->direction === 'mirror' ? 'Mirror (deletes extras)' : 'One-way copy' }}
                                @if ($rule->last_run_at)
                                    · Last ran {{ $rule->last_run_at->diffForHumans() }}
                                @endif
                            </p>
                        </div>
                        <div class="flex items-center gap-2 shrink-0">
                            <button wire:click="toggle({{ $rule->id }})" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                {{ $rule->enabled ? 'Pause' : 'Resume' }}
                            </button>
                            <button wire:click="openEdit({{ $rule->id }})" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-stone-600 hover:bg-stone-50 transition">
                                Edit
                            </button>
                            <button wire:click="delete({{ $rule->id }})" wire:confirm="Delete this sync rule?" class="text-xs font-medium px-3 py-1.5 rounded-md border border-stone-200 text-red-600 hover:bg-red-50 transition">
                                Delete
                            </button>
                        </div>
                    </div>
                @endforeach
            </div>
        @endif
    </div>

    <x-modal name="sync-rule-form" :show="$errors->isNotEmpty()" maxWidth="lg">
        <form wire:submit="save" class="p-6">
            <h3 class="text-lg font-semibold text-stone-800 mb-4">
                {{ $editingId ? 'Edit sync rule' : 'New sync rule' }}
            </h3>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div class="sm:col-span-2">
                    <x-input-label for="name" value="Name" />
                    <x-text-input id="name" wire:model="name" class="mt-1 block w-full" placeholder="Photos backup" />
                    <x-input-error :messages="$errors->get('name')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="sourceMachineId" value="Source machine" />
                    <select id="sourceMachineId" wire:model="sourceMachineId" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                        <option value="">Select…</option>
                        @foreach ($machines as $machine)
                            <option value="{{ $machine->id }}">{{ $machine->name }}</option>
                        @endforeach
                    </select>
                    <x-input-error :messages="$errors->get('sourceMachineId')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="sourcePath" value="Source path" />
                    <x-text-input id="sourcePath" wire:model="sourcePath" class="mt-1 block w-full font-mono text-sm" placeholder="/home/andrew/Photos" />
                    <x-input-error :messages="$errors->get('sourcePath')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="destinationMachineId" value="Destination machine" />
                    <select id="destinationMachineId" wire:model="destinationMachineId" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                        <option value="">Select…</option>
                        @foreach ($machines as $machine)
                            <option value="{{ $machine->id }}">{{ $machine->name }}</option>
                        @endforeach
                    </select>
                    <x-input-error :messages="$errors->get('destinationMachineId')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="destinationPath" value="Destination path" />
                    <x-text-input id="destinationPath" wire:model="destinationPath" class="mt-1 block w-full font-mono text-sm" placeholder="D:\Backups\Photos" />
                    <x-input-error :messages="$errors->get('destinationPath')" class="mt-1" />
                </div>

                <div>
                    <x-input-label for="direction" value="Direction" />
                    <select id="direction" wire:model="direction" class="mt-1 block w-full border-stone-300 rounded-md shadow-sm focus:border-tan-400 focus:ring-tan-400">
                        <option value="one_way">One-way (copy only)</option>
                        <option value="mirror">Mirror (also deletes extras at destination)</option>
                    </select>
                </div>

                <div>
                    <x-input-label for="intervalMinutes" value="Run every (minutes)" />
                    <x-text-input id="intervalMinutes" type="number" wire:model="intervalMinutes" class="mt-1 block w-full" />
                    <x-input-error :messages="$errors->get('intervalMinutes')" class="mt-1" />
                </div>

                <div class="sm:col-span-2 flex items-center gap-2">
                    <input id="enabled" type="checkbox" wire:model="enabled" class="rounded border-stone-300 text-tan-500 focus:ring-tan-400">
                    <x-input-label for="enabled" value="Enabled" class="!mb-0" />
                </div>
            </div>

            <div class="mt-6 flex justify-end gap-3">
                <button type="button" x-on:click="$dispatch('close-modal', 'sync-rule-form')" class="px-4 py-2 text-sm font-medium text-stone-600 hover:text-stone-800">Cancel</button>
                <x-primary-button>{{ $editingId ? 'Save changes' : 'Create rule' }}</x-primary-button>
            </div>
        </form>
    </x-modal>
</div>
