<?php

namespace App\Models;

use App\Services\AgentClient;
use Database\Factories\MachineFactory;
use Illuminate\Database\Eloquent\Builder;
use Illuminate\Database\Eloquent\Casts\Attribute;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsToMany;
use Illuminate\Database\Eloquent\Relations\HasMany;

class Machine extends Model
{
    /** @use HasFactory<MachineFactory> */
    use HasFactory;

    protected $fillable = [
        'name',
        'host',
        'port',
        'os',
        'agent_token',
        'color',
        'icon',
        'sort_order',
        'use_tls',
        'status',
        'last_seen_at',
        'low_space_alerted_at',
        'notes',
    ];

    protected function casts(): array
    {
        return [
            'agent_token' => 'encrypted',
            'use_tls' => 'boolean',
            'last_seen_at' => 'datetime',
            'low_space_alerted_at' => 'datetime',
        ];
    }

    public function users(): BelongsToMany
    {
        return $this->belongsToMany(User::class)->withPivot('role')->withTimestamps();
    }

    public function diskReadings(): HasMany
    {
        return $this->hasMany(DiskReading::class);
    }

    /**
     * @param  Builder<Machine>  $query
     * @return Builder<Machine>
     */
    public function scopeAccessibleTo(Builder $query, User $user): Builder
    {
        return $query->whereHas('users', fn ($q) => $q->where('user_id', $user->id));
    }

    protected function baseUrl(): Attribute
    {
        return Attribute::get(fn () => sprintf(
            '%s://%s:%d',
            $this->use_tls ? 'https' : 'http',
            $this->host,
            $this->port,
        ));
    }

    public function isOnline(): bool
    {
        return $this->status === 'online';
    }

    public function agent(): AgentClient
    {
        return new AgentClient($this);
    }
}
