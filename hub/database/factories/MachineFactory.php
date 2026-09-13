<?php

namespace Database\Factories;

use App\Models\Machine;
use Illuminate\Database\Eloquent\Factories\Factory;

/**
 * @extends Factory<Machine>
 */
class MachineFactory extends Factory
{
    /**
     * Define the model's default state.
     *
     * @return array<string, mixed>
     */
    public function definition(): array
    {
        return [
            'name' => fake()->unique()->word().' Machine',
            'host' => fake()->localIpv4(),
            'port' => 8765,
            'os' => fake()->randomElement(['windows', 'linux', 'macos']),
            'agent_token' => fake()->sha256(),
            'color' => fake()->safeHexColor(),
        ];
    }
}
